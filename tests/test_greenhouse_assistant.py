from __future__ import annotations

import json
import unittest
from unittest import mock

from career_monitor.greenhouse_assistant import (
    _greenhouse_slm_model,
    _call_greenhouse_slm,
    _greenhouse_slm_question_context,
    _greenhouse_slm_timeout_seconds,
    _extract_rendered_dom_questions,
    _selector_from_name,
    analyze_greenhouse_application,
    build_approved_answer_targets,
    build_autofill_targets,
    build_interactive_suggested_answer_targets,
    build_suggested_answer_targets,
    detect_greenhouse_application,
    GreenhouseApplicationSchema,
    GreenhouseSuggestedAnswer,
    GreenhouseResumeRecommendation,
    load_greenhouse_application_schema,
    recommend_resume_selection,
)
from career_monitor.profile_knowledge import ProfileRetrievalItem, ProfileRetrievalResult


def sample_api_payload(*, with_consent: bool = True) -> dict:
    payload = {
        "company_name": "Example Co",
        "title": "Software Engineer",
        "absolute_url": "https://job-boards.greenhouse.io/example/jobs/12345",
        "location": {"name": "Remote"},
        "questions": [
            {
                "label": "First Name",
                "required": True,
                "fields": [{"name": "first_name", "type": "input_text", "values": []}],
            },
            {
                "label": "Email",
                "required": True,
                "fields": [{"name": "email", "type": "input_text", "values": []}],
            },
            {
                "label": "Resume/CV",
                "required": True,
                "fields": [
                    {"name": "resume", "type": "input_file", "values": []},
                    {"name": "resume_text", "type": "textarea", "values": []},
                ],
            },
            {
                "label": "Why do you want to work here?",
                "required": False,
                "fields": [{"name": "question_1", "type": "textarea", "values": []}],
            },
            {
                "label": "Will you now or in the future require visa sponsorship?",
                "required": True,
                "fields": [
                    {
                        "name": "question_2",
                        "type": "multi_value_single_select",
                        "values": [
                            {"label": "Yes", "value": "yes"},
                            {"label": "No", "value": "no"},
                        ],
                    }
                ],
            },
        ],
        "location_questions": [],
        "compliance": [
            {
                "type": "eeoc",
                "questions": [
                    {
                        "label": "Gender",
                        "required": False,
                        "fields": [
                            {
                                "name": "gender",
                                "type": "multi_value_single_select",
                                "values": [
                                    {"label": "Female", "value": "f"},
                                    {"label": "Male", "value": "m"},
                                ],
                            }
                        ],
                    }
                ],
            }
        ],
        "data_compliance": [],
    }
    if with_consent:
        payload["data_compliance"] = [
            {
                "type": "gdpr",
                "requires_consent": False,
                "requires_processing_consent": True,
                "requires_retention_consent": False,
                "demographic_data_consent_applies": False,
            }
        ]
    return payload


def sample_hidden_location_payload() -> dict:
    return {
        "company_name": "Example Co",
        "title": "Software Engineer",
        "absolute_url": "https://job-boards.greenhouse.io/example/jobs/12345",
        "location": {"name": "Remote"},
        "questions": [
            {
                "label": "First Name",
                "required": True,
                "fields": [{"name": "first_name", "type": "input_text", "values": []}],
            },
            {
                "label": "Email",
                "required": True,
                "fields": [{"name": "email", "type": "input_text", "values": []}],
            },
            {
                "label": "Resume/CV",
                "required": True,
                "fields": [
                    {"name": "resume", "type": "input_file", "values": []},
                    {"name": "resume_text", "type": "textarea", "values": []},
                ],
            },
        ],
        "location_questions": [
            {
                "label": "Location",
                "required": True,
                "fields": [{"name": "location", "type": "input_text", "values": []}],
            },
            {
                "label": "Latitude",
                "required": True,
                "fields": [{"name": "latitude", "type": "input_hidden", "values": []}],
            },
            {
                "label": "Longitude",
                "required": True,
                "fields": [{"name": "longitude", "type": "input_hidden", "values": []}],
            },
        ],
        "compliance": [],
        "data_compliance": [],
    }


def sample_taxonomy_payload() -> dict:
    return {
        "company_name": "Example Co",
        "title": "AI Engineer",
        "absolute_url": "https://job-boards.greenhouse.io/example/jobs/99999",
        "location": {"name": "Remote"},
        "questions": [
            {
                "label": "Preferred First Name",
                "required": False,
                "fields": [{"name": "question_pref", "type": "input_text", "values": []}],
            },
            {
                "label": "LinkedIn Profile",
                "required": False,
                "fields": [{"name": "question_linkedin", "type": "textarea", "values": []}],
            },
            {
                "label": "Why Anthropic?",
                "required": True,
                "fields": [{"name": "question_why", "type": "input_text", "values": []}],
            },
            {
                "label": "Please review and acknowledge Cloudflare's Candidate Privacy Policy (cloudflare.com/candidate-privacy-notice/).",
                "required": True,
                "fields": [
                    {
                        "name": "question_privacy",
                        "type": "multi_value_multi_select",
                        "values": [{"label": "I acknowledge", "value": "ack"}],
                    }
                ],
            },
        ],
        "location_questions": [],
        "compliance": [],
        "data_compliance": [],
    }


def sample_policy_payload() -> dict:
    return {
        "company_name": "Example Co",
        "title": "Applied AI Engineer",
        "absolute_url": "https://job-boards.greenhouse.io/example/jobs/77777",
        "location": {"name": "Remote"},
        "content": "We are hiring an Applied AI Engineer to build LLM systems, evaluation pipelines, and agentic workflows on cloud infrastructure.",
        "questions": [
            {
                "label": "Resume/CV",
                "required": True,
                "fields": [
                    {"name": "resume", "type": "input_file", "values": []},
                    {"name": "resume_text", "type": "textarea", "values": []},
                ],
            },
            {
                "label": "What is your current visa status?",
                "required": True,
                "fields": [{"name": "question_visa_status", "type": "input_text", "values": []}],
            },
            {
                "label": "Will you now or in the future require visa sponsorship? (This includes F-1 OPT and STEM OPT.)",
                "required": True,
                "fields": [
                    {
                        "name": "question_sponsorship",
                        "type": "multi_value_single_select",
                        "values": [
                            {"label": "Yes", "value": "yes"},
                            {"label": "No", "value": "no"},
                        ],
                    }
                ],
            },
        ],
        "location_questions": [],
        "compliance": [],
        "data_compliance": [],
    }


def sample_grvty_policy_payload() -> dict:
    return {
        "company_name": "GRVTY",
        "title": "Software Developer (Systems Software)",
        "absolute_url": "https://job-boards.greenhouse.io/grvty/jobs/4178136009",
        "location": {"name": "McLean, Virginia, United States"},
        "content": (
            "<p>GRVTY is seeking a Software Developer with a <strong>TS/SCI + Poly clearance</strong>.</p>"
            "<p>Active TS/SCI with Polygraph Clearance required.</p>"
        ),
        "questions": [
            {
                "label": "Employment Eligibility Information",
                "required": True,
                "fields": [
                    {
                        "name": "question_work_auth",
                        "type": "multi_value_single_select",
                        "values": [
                            {"label": "Yes", "value": "1"},
                            {"label": "No", "value": "0"},
                        ],
                    }
                ],
                "description": "Are you authorized to work in the United States for any employer?",
            },
            {
                "label": "Visa Requirement Status",
                "required": True,
                "fields": [
                    {
                        "name": "question_visa",
                        "type": "multi_value_single_select",
                        "values": [
                            {"label": "Yes", "value": "1"},
                            {"label": "No", "value": "0"},
                        ],
                    }
                ],
                "description": "Will you now or in the future require visa sponsorship?",
            },
            {
                "label": "Security Clearance Type",
                "required": True,
                "description": (
                    "U.S. citizenship is a basic security clearance eligibility requirement. "
                    "We are unable to sponsor candidates for a U.S. Security Clearance."
                ),
                "fields": [
                    {
                        "name": "question_clearance",
                        "type": "multi_value_single_select",
                        "values": [
                            {"label": "No Clearance", "value": "nc"},
                            {"label": "TS/SCI", "value": "ts"},
                        ],
                    }
                ],
            },
        ],
        "location_questions": [],
        "compliance": [],
        "data_compliance": [],
    }


def sample_cloudflare_like_payload() -> dict:
    return {
        "company_name": "Cloudflare",
        "title": "Systems Engineer, Frontend",
        "absolute_url": "https://boards.greenhouse.io/cloudflare/jobs/7436794?gh_jid=7436794",
        "location": {"name": "Remote"},
        "questions": [
            {
                "label": "First Name",
                "required": True,
                "fields": [{"name": "first_name", "type": "input_text", "values": []}],
            },
            {
                "label": "Last Name",
                "required": True,
                "fields": [{"name": "last_name", "type": "input_text", "values": []}],
            },
            {
                "label": "Legal Name (if different than above)",
                "required": False,
                "fields": [{"name": "question_legal_name", "type": "input_text", "values": []}],
            },
            {
                "label": "Would you like to include your LinkedIn profile, personal website or blog?",
                "required": False,
                "fields": [{"name": "question_profile_link", "type": "input_text", "values": []}],
            },
            {
                "label": "How did you hear about this job?",
                "required": True,
                "fields": [
                    {
                        "name": "question_referral",
                        "type": "multi_value_single_select",
                        "values": [
                            {"label": "Company Website", "value": "company_website"},
                            {"label": "LinkedIn", "value": "linkedin"},
                        ],
                    }
                ],
            },
        ],
        "location_questions": [],
        "compliance": [],
        "data_compliance": [],
    }


def sample_anthropic_like_payload() -> dict:
    return {
        "company_name": "Anthropic",
        "title": "Full Stack Software Engineer, Reinforcement Learning",
        "absolute_url": "https://job-boards.greenhouse.io/anthropic/jobs/5098984008",
        "location": {"name": "San Francisco, CA"},
        "content": "Anthropic is building reliable AI systems. We value thoughtful collaboration with Claude, strong engineering, and clear communication.",
        "questions": [
            {
                "label": "When is the earliest you would want to start working with us?",
                "required": False,
                "fields": [{"name": "question_start", "type": "input_text", "values": []}],
            },
            {
                "label": "Do you have any deadlines or timeline considerations we should be aware of?",
                "required": False,
                "fields": [{"name": "question_timeline", "type": "input_text", "values": []}],
            },
            {
                "label": "Are you open to working in-person in one of our offices 25% of the time?",
                "required": True,
                "fields": [
                    {
                        "name": "question_onsite",
                        "type": "multi_value_single_select",
                        "values": [{"label": "Yes", "value": "yes"}, {"label": "No", "value": "no"}],
                    }
                ],
            },
            {
                "label": "AI Policy for Application",
                "required": True,
                "description": "Please review our AI partnership guidelines and confirm your understanding by selecting Yes.",
                "fields": [
                    {
                        "name": "question_ai_policy",
                        "type": "multi_value_single_select",
                        "values": [{"label": "Yes", "value": "yes"}, {"label": "No", "value": "no"}],
                    }
                ],
            },
            {
                "label": "Why Anthropic?",
                "required": True,
                "description": "Why do you want to work at Anthropic?",
                "fields": [{"name": "question_why", "type": "textarea", "values": []}],
            },
            {
                "label": "Additional Information",
                "required": False,
                "description": "Add a cover letter or anything else you want to share.",
                "fields": [{"name": "question_additional", "type": "textarea", "values": []}],
            },
            {
                "label": "What is the address from which you plan on working? If you would need to relocate, please type \"relocating\".",
                "required": False,
                "fields": [{"name": "question_address", "type": "input_text", "values": []}],
            },
            {
                "label": "Have you ever interviewed at Anthropic before?",
                "required": True,
                "fields": [
                    {
                        "name": "question_interviewed_before",
                        "type": "multi_value_single_select",
                        "values": [{"label": "Yes", "value": "yes"}, {"label": "No", "value": "no"}],
                    }
                ],
            },
        ],
        "location_questions": [],
        "compliance": [
            {
                "type": "eeoc",
                "questions": [
                    {
                        "label": "Gender",
                        "required": False,
                        "fields": [
                            {
                                "name": "gender",
                                "type": "multi_value_single_select",
                                "values": [
                                    {"label": "Decline To Self Identify", "value": "decline"},
                                    {"label": "Female", "value": "female"},
                                    {"label": "Male", "value": "male"},
                                ],
                            }
                        ],
                    },
                    {
                        "label": "Are you Hispanic/Latino?",
                        "required": False,
                        "fields": [
                            {
                                "name": "hispanic_ethnicity",
                                "type": "multi_value_single_select",
                                "values": [
                                    {"label": "Yes", "value": "yes"},
                                    {"label": "No", "value": "no"},
                                    {"label": "I do not wish to answer", "value": "decline"},
                                ],
                            }
                        ],
                    },
                    {
                        "label": "Race",
                        "required": False,
                        "fields": [
                            {
                                "name": "race",
                                "type": "multi_value_single_select",
                                "values": [
                                    {"label": "Decline To Self Identify", "value": "decline"},
                                    {"label": "Asian", "value": "asian"},
                                    {"label": "White", "value": "white"},
                                ],
                            }
                        ],
                    },
                    {
                        "label": "Veteran Status",
                        "required": False,
                        "fields": [
                            {
                                "name": "veteran_status",
                                "type": "multi_value_single_select",
                                "values": [
                                    {"label": "I don't wish to answer", "value": "decline"},
                                    {"label": "I am not a protected veteran", "value": "no"},
                                ],
                            }
                        ],
                    },
                ],
            }
        ],
        "data_compliance": [],
    }


def sample_preferred_name_payload() -> dict:
    return {
        "company_name": "Example Co",
        "title": "Software Engineer",
        "absolute_url": "https://job-boards.greenhouse.io/example/jobs/12345",
        "location": {"name": "Remote"},
        "questions": [
            {
                "label": "First Name",
                "required": True,
                "fields": [{"name": "first_name", "type": "input_text", "values": []}],
            },
            {
                "label": "Last Name",
                "required": True,
                "fields": [{"name": "last_name", "type": "input_text", "values": []}],
            },
            {
                "label": "Preferred First Name",
                "required": False,
                "fields": [{"name": "question_pref", "type": "input_text", "values": []}],
            },
        ],
        "location_questions": [],
        "compliance": [],
        "data_compliance": [],
    }


def sample_target_company_employment_payload() -> dict:
    return {
        "company_name": "Stripe",
        "title": "Software Engineer",
        "absolute_url": "https://job-boards.greenhouse.io/stripe/jobs/12345",
        "location": {"name": "Remote"},
        "questions": [
            {
                "label": "First Name",
                "required": True,
                "fields": [{"name": "first_name", "type": "input_text", "values": []}],
            },
            {
                "label": "Email",
                "required": True,
                "fields": [{"name": "email", "type": "input_text", "values": []}],
            },
            {
                "label": "Resume/CV",
                "required": True,
                "fields": [
                    {"name": "resume", "type": "input_file", "values": []},
                    {"name": "resume_text", "type": "textarea", "values": []},
                ],
            },
            {
                "label": "Have you ever been employed by Stripe or a Stripe affiliate?",
                "required": True,
                "fields": [
                    {
                        "name": "question_employment",
                        "type": "multi_value_single_select",
                        "values": [
                            {"label": "Yes", "value": "yes"},
                            {"label": "No", "value": "no"},
                        ],
                    }
                ],
            },
        ],
        "location_questions": [],
        "compliance": [],
        "data_compliance": [],
    }


def sample_grafana_payload() -> dict:
    return {
        "company_name": "Grafana Labs",
        "title": "Software Engineer - Platform InfraCore | USA | Remote",
        "absolute_url": "https://job-boards.greenhouse.io/grafanalabs/jobs/5809023004",
        "location": {"name": "United States (Remote)"},
        "content": "Platform InfraCore works on Kubernetes clusters, scheduling, autoscaling, Crossplane, and Terraform.",
        "questions": [
            {
                "label": "Are you located in and plan to work from the USA?",
                "required": True,
                "fields": [
                    {
                        "name": "question_located_usa",
                        "type": "multi_value_single_select",
                        "values": [
                            {"label": "Yes", "value": "1"},
                            {"label": "No", "value": "0"},
                        ],
                    }
                ],
            },
            {
                "label": "Are you currently eligible to work in your country of residence?",
                "required": True,
                "fields": [
                    {
                        "name": "question_work_eligible",
                        "type": "multi_value_single_select",
                        "values": [
                            {"label": "Yes", "value": "1"},
                            {"label": "No", "value": "0"},
                        ],
                    }
                ],
            },
            {
                "label": "Do you have experience with provisioning Kubernetes clusters and operating in production?",
                "required": True,
                "fields": [{"name": "question_k8s_provisioning", "type": "input_text", "values": []}],
            },
            {
                "label": "Which of the following best describes you?",
                "required": True,
                "fields": [
                    {
                        "name": "question_candidate_kind",
                        "type": "multi_value_single_select",
                        "values": [
                            {"label": "I am an AI or automated program", "value": "60669311004"},
                            {"label": "I am a human being", "value": "60669312004"},
                        ],
                    }
                ],
            },
            {
                "label": "How did you hear about this opportunity at Grafana?",
                "required": True,
                "fields": [
                    {
                        "name": "question_heard_about",
                        "type": "multi_value_single_select",
                        "values": [
                            {"label": "Built-In", "value": "built_in"},
                            {"label": "Company Website", "value": "company_site"},
                            {"label": "LinkedIn", "value": "linkedin"},
                            {"label": "Referral", "value": "referral"},
                        ],
                    }
                ],
            },
        ],
        "location_questions": [],
        "compliance": [],
        "data_compliance": [],
    }


def sample_cover_letter_payload() -> dict:
    return {
        "company_name": "Example Co",
        "title": "Solutions Engineer",
        "absolute_url": "https://job-boards.greenhouse.io/example/jobs/22222",
        "location": {"name": "Remote"},
        "questions": [
            {
                "label": "First Name",
                "required": True,
                "fields": [{"name": "first_name", "type": "input_text", "values": []}],
            },
            {
                "label": "Cover Letter",
                "required": True,
                "fields": [
                    {"name": "cover_letter", "type": "input_file", "values": []},
                    {"name": "cover_letter_text", "type": "textarea", "values": []},
                ],
            },
        ],
        "location_questions": [],
        "compliance": [],
        "data_compliance": [],
    }


def sample_optional_cover_letter_payload() -> dict:
    payload = sample_cover_letter_payload()
    payload["questions"][1]["required"] = False
    return payload


def sample_remix_html() -> str:
    context = {
        "state": {
            "loaderData": {
                "routes/$url_token_.jobs_.$job_post_id": {
                    "urlToken": "example",
                    "jobPostId": 12345,
                    "jobPost": {
                        "company_name": "Example Co",
                        "title": "Software Engineer",
                        "public_url": "https://job-boards.greenhouse.io/example/jobs/12345",
                        "job_post_location": "Remote",
                        "questions": [
                            {
                                "label": "LinkedIn Profile",
                                "required": False,
                                "fields": [
                                    {
                                        "name": "question_99",
                                        "type": "input_text",
                                        "values": [],
                                    }
                                ],
                            }
                        ],
                        "eeoc_sections": [
                            {
                                "questions": [
                                    {
                                        "label": "Veteran Status",
                                        "required": False,
                                        "fields": [
                                            {
                                                "name": "veteran_status",
                                                "type": "multi_value_single_select",
                                                "values": [
                                                    {"label": "Decline", "value": "decline"}
                                                ],
                                            }
                                        ],
                                    }
                                ]
                            }
                        ],
                    },
                }
            }
        }
    }
    return (
        "<html><body><form id=\"application-form\"></form>"
        f"<script>window.__remixContext = {json.dumps(context)}; </script>"
        "</body></html>"
    )


def sample_rendered_dom_supplement_html() -> str:
    return """
    <html>
      <body>
        <form id="application-form">
          <div class="eeoc__question__wrapper">
            <div class="select__container">
              <label id="gender-label" for="gender" class="label select__label">Gender</label>
              <div class="select__input-container">
                <input id="gender" role="combobox" type="text" aria-labelledby="gender-label" />
              </div>
            </div>
          </div>
          <div class="eeoc__question__wrapper">
            <div class="select__container">
              <label id="hispanic_ethnicity-label" for="hispanic_ethnicity" class="label select__label">Are you Hispanic/Latino?</label>
              <div class="select__input-container">
                <input id="hispanic_ethnicity" role="combobox" type="text" aria-labelledby="hispanic_ethnicity-label" />
              </div>
            </div>
          </div>
          <div class="eeoc__question__wrapper">
            <div class="select__container">
              <label id="race-label" for="race" class="label select__label">Race</label>
              <div class="select__input-container">
                <input id="race" role="combobox" type="text" aria-labelledby="race-label" />
              </div>
            </div>
          </div>
          <div class="eeoc__question__wrapper">
            <div class="select__container">
              <label id="veteran_status-label" for="veteran_status" class="label select__label">Veteran Status</label>
              <div class="select__input-container">
                <input id="veteran_status" role="combobox" type="text" aria-labelledby="veteran_status-label" />
              </div>
            </div>
          </div>
        </form>
      </body>
    </html>
    """


def sample_hosted_form_html() -> str:
    return """
    <form id="new_form_submission_2_4" class="form-template">
      <div class="form-group form-template-field-string first-name">
        <label for="form_first_name_2_4_0" class="form-template-field-label">
          <span class="ada-label-text rich-text-label">First Name <small class="question-label-required">(required)</small></span>
        </label>
        <input id="form_first_name_2_4_0" type="text" required="required" name="form_submission[fields_attributes][0][string_value]" />
      </div>
      <div class="form-group form-template-field-email email">
        <label for="form_email_2_4_2" class="form-template-field-label">
          <span class="ada-label-text rich-text-label">Email <small class="question-label-required">(required)</small></span>
        </label>
        <input id="form_email_2_4_2" type="email" required="required" name="form_submission[fields_attributes][2][email_value]" />
      </div>
      <div class="form-group form-template-field-file resume">
        <label for="question_2_4_4_0_0" class="form-template-field-label">
          <span class="ada-label-text rich-text-label">Resume/CV <small class="question-label-required">(required)</small></span>
        </label>
        <input id="question_2_4_4_0_0" type="file" required="required" name="form_submission[fields_attributes][4][files][]" />
      </div>
      <div class="form-group form-template-field-select hear-about">
        <label for="form_hear_about_2_4_5" class="form-template-field-label">
          <span class="ada-label-text rich-text-label">How did you hear about us? <small class="question-label-required">(required)</small></span>
        </label>
        <select id="form_hear_about_2_4_5" name="form_submission[fields_attributes][5][string_value]" required="required">
          <option value=""></option>
          <option value="linkedin">LinkedIn</option>
          <option value="other">Other</option>
        </select>
      </div>
      <button type="submit">Submit Application</button>
    </form>
    """


class GreenhouseAssistantTests(unittest.TestCase):
    @mock.patch.dict("os.environ", {}, clear=True)
    def test_greenhouse_slm_defaults_to_qwen3_4b(self) -> None:
        self.assertEqual(_greenhouse_slm_model(), "qwen3:4b")
        self.assertEqual(_greenhouse_slm_timeout_seconds(), 15)

    @mock.patch.dict("os.environ", {"GREENHOUSE_SLM_MODEL": "qwen2.5:3b"}, clear=True)
    def test_greenhouse_slm_timeout_stays_shorter_for_non_qwen3_models(self) -> None:
        self.assertEqual(_greenhouse_slm_model(), "qwen2.5:3b")
        self.assertEqual(_greenhouse_slm_timeout_seconds(), 8)

    def test_selector_from_name_supports_hosted_location_and_checkbox_groups(self) -> None:
        self.assertEqual(
            _selector_from_name("location"),
            "#location, [name='location'], #candidate-location, #candidate_location, [name='candidate_location']",
        )
        self.assertEqual(
            _selector_from_name("question_58651459[]"),
            "#question_58651459, #question-58651459, [name='question_58651459[]']",
        )

    def test_detect_hosted_greenhouse_job(self) -> None:
        detection = detect_greenhouse_application(
            "https://job-boards.greenhouse.io/example/jobs/12345"
        )
        self.assertTrue(detection.is_greenhouse)
        self.assertTrue(detection.is_application)
        self.assertEqual(detection.board_token, "example")
        self.assertEqual(detection.job_id, "12345")

    def test_detect_embed_job_application_path(self) -> None:
        detection = detect_greenhouse_application(
            "https://boards.greenhouse.io/embed/job_app?token=abc123"
        )
        self.assertTrue(detection.is_greenhouse)
        self.assertTrue(detection.is_application)
        self.assertIsNone(detection.board_token)
        self.assertIsNone(detection.job_id)

    def test_detect_custom_hosted_greenhouse_job_with_query_signals(self) -> None:
        detection = detect_greenhouse_application(
            "https://www.weareroku.com/jobs/7680250?gh_jid=7680250&gh_src=my.greenhouse.search"
        )
        self.assertTrue(detection.is_greenhouse)
        self.assertTrue(detection.is_application)
        self.assertIsNone(detection.board_token)
        self.assertEqual(detection.job_id, "7680250")
        self.assertIn("custom_hosted_job_path", detection.signals)

    def test_extract_rendered_dom_questions_supports_form_template(self) -> None:
        questions = _extract_rendered_dom_questions(sample_hosted_form_html())

        labels = [question.label for question in questions]
        self.assertIn("First Name", labels)
        self.assertIn("Email", labels)
        self.assertIn("Resume/CV", labels)
        self.assertIn("How did you hear about us?", labels)
        hear_about = next(question for question in questions if question.label == "How did you hear about us?")
        self.assertEqual(hear_about.inputs[0].ui_type, "combobox")
        self.assertEqual([option.label for option in hear_about.inputs[0].options], ["LinkedIn", "Other"])

    @mock.patch("career_monitor.greenhouse_assistant._fetch_greenhouse_api_payload")
    def test_greenhouse_slm_question_context_includes_neighbors_and_input_metadata(
        self,
        mock_fetch_api: mock.Mock,
    ) -> None:
        mock_fetch_api.return_value = sample_api_payload()
        schema = load_greenhouse_application_schema(
            "https://job-boards.greenhouse.io/example/jobs/12345",
            session=object(),  # type: ignore[arg-type]
        )

        context = _greenhouse_slm_question_context(schema, schema.questions[3])

        self.assertEqual(context["label"], "Why do you want to work here?")
        self.assertEqual(context["primary_input"]["ui_type"], "textarea")
        self.assertEqual(context["primary_input"]["api_name"], "question_1")
        self.assertEqual(context["neighboring_questions"]["previous"], ["Email", "Resume/CV"])
        self.assertIn(
            "Will you now or in the future require visa sponsorship?",
            context["neighboring_questions"]["next"],
        )

    @mock.patch("career_monitor.greenhouse_assistant._fetch_greenhouse_api_payload")
    def test_recommend_resume_selection_prefers_explicit_variant(
        self,
        mock_fetch_api: mock.Mock,
    ) -> None:
        mock_fetch_api.return_value = sample_policy_payload()
        schema = load_greenhouse_application_schema(
            "https://job-boards.greenhouse.io/example/jobs/77777",
            session=object(),  # type: ignore[arg-type]
        )

        selection = recommend_resume_selection(
            {
                "resume_variants": {
                    "ai": "/tmp/resume-ai.pdf",
                    "cloud": "/tmp/resume-cloud.pdf",
                },
                "selected_resume_variant": "cloud",
                "resume_selection_source": "manual_override",
            },
            schema,
        )

        self.assertEqual(selection.variant, "cloud")
        self.assertEqual(selection.path, "/tmp/resume-cloud.pdf")
        self.assertEqual(selection.source, "manual_override")

    @mock.patch("career_monitor.greenhouse_assistant._call_greenhouse_resume_slm")
    @mock.patch("career_monitor.greenhouse_assistant._fetch_greenhouse_api_payload")
    def test_recommend_resume_selection_uses_slm_variant_when_available(
        self,
        mock_fetch_api: mock.Mock,
        mock_resume_slm: mock.Mock,
    ) -> None:
        mock_fetch_api.return_value = sample_policy_payload()
        schema = load_greenhouse_application_schema(
            "https://job-boards.greenhouse.io/example/jobs/77777",
            session=object(),  # type: ignore[arg-type]
        )
        mock_resume_slm.return_value = GreenhouseResumeRecommendation(
            variant="cloud",
            path="/tmp/resume-cloud.pdf",
            source="slm_recommended",
            available_variants=[
                {"variant": "ai", "label": "Ai", "path": "/tmp/resume-ai.pdf", "file": "resume-ai.pdf"},
                {"variant": "cloud", "label": "Cloud", "path": "/tmp/resume-cloud.pdf", "file": "resume-cloud.pdf"},
            ],
            reason="The job emphasizes infrastructure and platform engineering.",
            confidence=0.84,
        )

        selection = recommend_resume_selection(
            {
                "resume_variants": {
                    "ai": "/tmp/resume-ai.pdf",
                    "cloud": "/tmp/resume-cloud.pdf",
                },
                "resume_variant_default": "ai",
            },
            schema,
        )

        self.assertEqual(selection.variant, "cloud")
        self.assertEqual(selection.path, "/tmp/resume-cloud.pdf")
        self.assertEqual(selection.source, "slm_recommended")
        self.assertEqual(selection.reason, "The job emphasizes infrastructure and platform engineering.")
        self.assertEqual(selection.confidence, 0.84)
        mock_resume_slm.assert_called_once()

    @mock.patch("career_monitor.greenhouse_assistant._fetch_greenhouse_api_payload")
    def test_load_schema_from_api(self, mock_fetch_api: mock.Mock) -> None:
        mock_fetch_api.return_value = sample_api_payload()

        schema = load_greenhouse_application_schema(
            "https://job-boards.greenhouse.io/example/jobs/12345",
            session=object(),  # type: ignore[arg-type]
        )

        self.assertEqual(schema.source, "job_board_api")
        self.assertEqual(schema.company_name, "Example Co")
        self.assertEqual(schema.title, "Software Engineer")
        self.assertEqual(schema.job_location, "Remote")
        labels = [question.label for question in schema.questions]
        self.assertIn("First Name", labels)
        self.assertIn("Gender", labels)
        self.assertIn("Consent to processing of personal data", labels)

    @mock.patch("career_monitor.greenhouse_assistant._fetch_html")
    @mock.patch("career_monitor.greenhouse_assistant._fetch_greenhouse_api_payload")
    def test_load_schema_from_api_merges_rendered_dom_only_questions(
        self,
        mock_fetch_api: mock.Mock,
        mock_fetch_html: mock.Mock,
    ) -> None:
        mock_fetch_api.return_value = sample_api_payload()
        mock_fetch_html.return_value = sample_rendered_dom_supplement_html()

        schema = load_greenhouse_application_schema(
            "https://job-boards.greenhouse.io/example/jobs/12345",
            session=object(),  # type: ignore[arg-type]
        )

        labels = [question.label for question in schema.questions]
        self.assertIn("Are you Hispanic/Latino?", labels)
        self.assertEqual(labels.count("Gender"), 1)
        self.assertEqual(schema.source, "job_board_api+html_dom")

    @mock.patch("career_monitor.greenhouse_assistant._fetch_html")
    @mock.patch("career_monitor.greenhouse_assistant._fetch_greenhouse_api_payload")
    def test_load_schema_from_html_fallback(
        self,
        mock_fetch_api: mock.Mock,
        mock_fetch_html: mock.Mock,
    ) -> None:
        mock_fetch_api.side_effect = RuntimeError("api unavailable")
        mock_fetch_html.return_value = sample_remix_html()

        schema = load_greenhouse_application_schema(
            "https://job-boards.greenhouse.io/example/jobs/12345",
            session=object(),  # type: ignore[arg-type]
        )

        self.assertEqual(schema.source, "html_remix_context")
        self.assertEqual(schema.detection.board_token, "example")
        self.assertEqual(schema.detection.job_id, "12345")
        labels = [question.label for question in schema.questions]
        self.assertIn("LinkedIn Profile", labels)
        self.assertIn("Veteran Status", labels)

    @mock.patch("career_monitor.greenhouse_assistant._fetch_html")
    def test_load_schema_from_custom_hosted_html_fallback(
        self,
        mock_fetch_html: mock.Mock,
    ) -> None:
        mock_fetch_html.return_value = sample_remix_html()

        schema = load_greenhouse_application_schema(
            "https://www.weareroku.com/jobs/7680250?gh_jid=7680250&gh_src=my.greenhouse.search",
            session=object(),  # type: ignore[arg-type]
        )

        self.assertEqual(schema.source, "html_remix_context")
        self.assertEqual(schema.detection.board_token, "example")
        self.assertEqual(schema.detection.job_id, "12345")
        self.assertEqual(schema.company_name, "Example Co")

    @mock.patch("career_monitor.greenhouse_assistant._load_greenhouse_application_schema_with_playwright")
    @mock.patch("career_monitor.greenhouse_assistant._fetch_html")
    def test_load_schema_uses_playwright_fallback_for_custom_hosted_page(
        self,
        mock_fetch_html: mock.Mock,
        mock_playwright_schema: mock.Mock,
    ) -> None:
        url = "https://www.weareroku.com/jobs/7680250?gh_jid=7680250&gh_src=my.greenhouse.search"
        mock_fetch_html.return_value = "<html><body>challenge</body></html>"
        mock_playwright_schema.return_value = GreenhouseApplicationSchema(
            source="playwright_dom",
            detection=detect_greenhouse_application(url),
            company_name="Roku",
            title="Software Engineer",
            public_url=url,
            job_location="San Jose, California, United States",
            job_content="<p>Teamwork makes the stream work.</p>",
            questions=_extract_rendered_dom_questions(sample_hosted_form_html()),
        )

        schema = load_greenhouse_application_schema(
            url,
            session=object(),  # type: ignore[arg-type]
        )

        self.assertEqual(schema.source, "playwright_dom")
        self.assertEqual(schema.company_name, "Roku")
        mock_playwright_schema.assert_called_once()

    @mock.patch("career_monitor.greenhouse_assistant._fetch_greenhouse_api_payload")
    def test_analysis_classifies_questions_deterministically(self, mock_fetch_api: mock.Mock) -> None:
        mock_fetch_api.return_value = sample_api_payload()
        schema = load_greenhouse_application_schema(
            "https://job-boards.greenhouse.io/example/jobs/12345",
            session=object(),  # type: ignore[arg-type]
        )
        analysis = analyze_greenhouse_application(
            schema,
            profile={
                "first_name": "Ada",
                "resume_path": "/tmp/resume.pdf",
            },
        )

        actions = {question.label: question.decision.action for question in analysis.schema.questions}
        self.assertEqual(actions["First Name"], "AUTOFILL")
        self.assertEqual(actions["Email"], "BLOCKED")
        self.assertEqual(actions["Resume/CV"], "AUTOFILL")
        self.assertEqual(actions["Why do you want to work here?"], "QUEUE")
        self.assertEqual(
            actions["Will you now or in the future require visa sponsorship?"],
            "REVIEW",
        )
        self.assertEqual(actions["Gender"], "SKIP_OPTIONAL")
        self.assertEqual(actions["Consent to processing of personal data"], "REVIEW")
        self.assertFalse(analysis.auto_submit_eligible)

    @mock.patch("career_monitor.greenhouse_assistant._fetch_html")
    @mock.patch("career_monitor.greenhouse_assistant._fetch_greenhouse_api_payload")
    def test_dom_supplement_demographic_questions_generate_fill_targets(
        self,
        mock_fetch_api: mock.Mock,
        mock_fetch_html: mock.Mock,
    ) -> None:
        mock_fetch_api.return_value = sample_api_payload()
        mock_fetch_html.return_value = sample_rendered_dom_supplement_html()
        schema = load_greenhouse_application_schema(
            "https://job-boards.greenhouse.io/example/jobs/12345",
            session=object(),  # type: ignore[arg-type]
        )
        profile = {
            "first_name": "Ada",
            "email": "ada@example.com",
            "resume_path": "/tmp/resume.pdf",
            "gender_answer": "Male",
            "hispanic_latino_answer": "Yes",
            "race_answer": "Asian",
            "veteran_status": "I am not a protected veteran",
        }
        analysis = analyze_greenhouse_application(schema, profile=profile)
        targets = build_approved_answer_targets(analysis, profile=profile)
        values = {target.question_label: target.value for target in targets}

        self.assertEqual(values["Are you Hispanic/Latino?"], "Yes")
        self.assertEqual(values["Gender"], "Male")
        self.assertEqual(values["Race"], "Asian")
        self.assertEqual(values["Veteran Status"], "I am not a protected veteran")

    @mock.patch("career_monitor.greenhouse_assistant._fetch_greenhouse_api_payload")
    def test_build_autofill_targets_keeps_text_and_file_inputs(self, mock_fetch_api: mock.Mock) -> None:
        mock_fetch_api.return_value = sample_api_payload()
        schema = load_greenhouse_application_schema(
            "https://job-boards.greenhouse.io/example/jobs/12345",
            session=object(),  # type: ignore[arg-type]
        )
        profile = {
            "first_name": "Ada",
            "email": "ada@example.com",
            "resume_path": "/tmp/resume.pdf",
        }
        analysis = analyze_greenhouse_application(schema, profile=profile)
        targets = build_autofill_targets(analysis, profile=profile)

        key_pairs = {(target.question_label, target.profile_key) for target in targets}
        self.assertIn(("First Name", "form_first_name"), key_pairs)
        self.assertIn(("Email", "email"), key_pairs)
        self.assertIn(("Resume/CV", "resume_path"), key_pairs)
        self.assertNotIn(("Why do you want to work here?", None), key_pairs)

    @mock.patch("career_monitor.greenhouse_assistant._fetch_greenhouse_api_payload")
    def test_submit_safety_requires_supported_answers_for_review_items(
        self,
        mock_fetch_api: mock.Mock,
    ) -> None:
        mock_fetch_api.return_value = sample_api_payload()
        schema = load_greenhouse_application_schema(
            "https://job-boards.greenhouse.io/example/jobs/12345",
            session=object(),  # type: ignore[arg-type]
        )
        analysis = analyze_greenhouse_application(
            schema,
            profile={
                "first_name": "Ada",
                "email": "ada@example.com",
                "resume_path": "/tmp/resume.pdf",
            },
            approved_answers={
                "Why do you want to work here?": "I like the mission.",
                "question_2": "No",
                "Consent to processing of personal data": "Yes",
            },
        )

        statuses = {item.question_label: item.status for item in analysis.review_queue}
        self.assertEqual(statuses["Why do you want to work here?"], "answered")
        self.assertEqual(
            statuses["Will you now or in the future require visa sponsorship?"],
            "answered",
        )
        self.assertEqual(statuses["Consent to processing of personal data"], "answered")
        self.assertTrue(analysis.auto_submit_eligible)

    @mock.patch("career_monitor.greenhouse_assistant._fetch_greenhouse_api_payload")
    def test_submit_safety_turns_true_when_supported_queue_is_fully_answered(
        self,
        mock_fetch_api: mock.Mock,
    ) -> None:
        mock_fetch_api.return_value = sample_api_payload(with_consent=False)
        schema = load_greenhouse_application_schema(
            "https://job-boards.greenhouse.io/example/jobs/12345",
            session=object(),  # type: ignore[arg-type]
        )
        analysis = analyze_greenhouse_application(
            schema,
            profile={
                "first_name": "Ada",
                "email": "ada@example.com",
                "resume_path": "/tmp/resume.pdf",
            },
            approved_answers={
                "Why do you want to work here?": "I like the mission.",
                "question_2": "No",
            },
        )
        approved_targets = build_approved_answer_targets(
            analysis,
            approved_answers={
                "Why do you want to work here?": "I like the mission.",
                "question_2": "No",
            },
        )

        self.assertTrue(analysis.auto_submit_eligible)
        ui_types = {(target.question_label, target.ui_type) for target in approved_targets}
        self.assertIn(("Why do you want to work here?", "textarea"), ui_types)
        self.assertIn(
            ("Will you now or in the future require visa sponsorship?", "combobox"),
            ui_types,
        )

    @mock.patch("career_monitor.greenhouse_assistant._fetch_greenhouse_api_payload")
    def test_hidden_location_fields_do_not_block_autonomous_submit(
        self,
        mock_fetch_api: mock.Mock,
    ) -> None:
        mock_fetch_api.return_value = sample_hidden_location_payload()
        schema = load_greenhouse_application_schema(
            "https://job-boards.greenhouse.io/example/jobs/12345",
            session=object(),  # type: ignore[arg-type]
        )
        analysis = analyze_greenhouse_application(
            schema,
            profile={
                "first_name": "Ada",
                "email": "ada@example.com",
                "resume_path": "/tmp/resume.pdf",
                "location": "Remote, United States",
            },
        )

        actions = {question.label: question.decision.action for question in analysis.schema.questions}
        self.assertEqual(actions["Latitude"], "SKIP_OPTIONAL")
        self.assertEqual(actions["Longitude"], "SKIP_OPTIONAL")
        self.assertEqual(actions["Location"], "AUTOFILL")
        self.assertTrue(analysis.auto_submit_eligible)

    @mock.patch("career_monitor.greenhouse_assistant._fetch_greenhouse_api_payload")
    def test_survey_taxonomy_handles_preferred_name_privacy_and_why_questions(
        self,
        mock_fetch_api: mock.Mock,
    ) -> None:
        mock_fetch_api.return_value = sample_taxonomy_payload()
        schema = load_greenhouse_application_schema(
            "https://job-boards.greenhouse.io/example/jobs/99999",
            session=object(),  # type: ignore[arg-type]
        )
        analysis = analyze_greenhouse_application(
            schema,
            profile={
                "first_name": "Ada",
                "linkedin": "https://linkedin.example/ada",
            },
        )
        targets = build_autofill_targets(
            analysis,
            profile={
                "first_name": "Ada",
                "linkedin": "https://linkedin.example/ada",
            },
        )

        actions = {question.label: question.decision.action for question in analysis.schema.questions}
        self.assertEqual(actions["Preferred First Name"], "AUTOFILL")
        self.assertEqual(actions["LinkedIn Profile"], "AUTOFILL")
        self.assertEqual(actions["Why Anthropic?"], "QUEUE")
        self.assertEqual(
            actions["Please review and acknowledge Cloudflare's Candidate Privacy Policy (cloudflare.com/candidate-privacy-notice/)."],
            "REVIEW",
        )
        target_types = {(target.question_label, target.ui_type, target.profile_key) for target in targets}
        self.assertIn(("Preferred First Name", "text_input", "form_preferred_first_name"), target_types)
        self.assertIn(("LinkedIn Profile", "textarea", "linkedin"), target_types)

    @mock.patch("career_monitor.greenhouse_assistant._fetch_greenhouse_api_payload")
    def test_policy_answers_support_f1_opt_rules_and_resume_variant_selection(
        self,
        mock_fetch_api: mock.Mock,
    ) -> None:
        mock_fetch_api.return_value = sample_policy_payload()
        schema = load_greenhouse_application_schema(
            "https://job-boards.greenhouse.io/example/jobs/77777",
            session=object(),  # type: ignore[arg-type]
        )
        profile = {
            "first_name": "Ada",
            "last_name": "Lovelace",
            "email": "ada@example.com",
            "phone": "+1 555 123 4567",
            "current_visa_status": "F-1 OPT",
            "requires_visa_sponsorship_now": False,
            "requires_visa_sponsorship_future": False,
            "requires_sponsorship_if_opt_included": True,
            "resume_variants": {
                "ai": "/tmp/resume-ai.pdf",
                "distributed_systems": "/tmp/resume-distributed.pdf",
                "cloud": "/tmp/resume-cloud.pdf",
            },
        }
        analysis = analyze_greenhouse_application(schema, profile=profile)
        approved_targets = build_approved_answer_targets(
            analysis,
            approved_answers=None,
            profile=profile,
        )
        autofill_targets = build_autofill_targets(analysis, profile=profile)

        statuses = {item.question_label: item.status for item in analysis.review_queue}
        self.assertEqual(statuses["What is your current visa status?"], "answered")
        self.assertEqual(
            statuses[
                "Will you now or in the future require visa sponsorship? (This includes F-1 OPT and STEM OPT.)"
            ],
            "answered",
        )
        self.assertTrue(analysis.auto_submit_eligible)
        approved_values = {target.question_label: target.value for target in approved_targets}
        approved_sources = {target.question_label: target.value_source for target in approved_targets}
        self.assertEqual(approved_values["What is your current visa status?"], "F-1 OPT")
        self.assertEqual(
            approved_values[
                "Will you now or in the future require visa sponsorship? (This includes F-1 OPT and STEM OPT.)"
            ],
            "Yes",
        )
        self.assertEqual(approved_sources["What is your current visa status?"], "policy")
        self.assertEqual(
            approved_sources[
                "Will you now or in the future require visa sponsorship? (This includes F-1 OPT and STEM OPT.)"
            ],
            "policy",
        )
        autofill_values = {target.question_label: target.value for target in autofill_targets}
        self.assertEqual(autofill_values["Resume/CV"], "/tmp/resume-ai.pdf")

    @mock.patch("career_monitor.greenhouse_assistant._fetch_greenhouse_api_payload")
    def test_submit_safety_adds_job_policy_blockers_for_clearance_and_no_sponsorship_roles(
        self,
        mock_fetch_api: mock.Mock,
    ) -> None:
        mock_fetch_api.return_value = sample_grvty_policy_payload()
        schema = load_greenhouse_application_schema(
            "https://job-boards.greenhouse.io/grvty/jobs/4178136009",
            session=object(),  # type: ignore[arg-type]
        )
        analysis = analyze_greenhouse_application(
            schema,
            profile={
                "current_visa_status": "F-1 OPT",
                "requires_sponsorship_if_opt_included": True,
                "authorized_to_work_in_us": True,
            },
        )

        self.assertIsNotNone(analysis.submit_safety)
        assert analysis.submit_safety is not None
        self.assertFalse(analysis.auto_submit_eligible)
        self.assertGreaterEqual(len(analysis.submit_safety.job_blockers), 2)
        self.assertTrue(
            any("U.S. citizenship" in blocker for blocker in analysis.submit_safety.job_blockers)
        )
        self.assertTrue(
            any("sponsorship is not available" in blocker for blocker in analysis.submit_safety.job_blockers)
        )

    @mock.patch("career_monitor.greenhouse_assistant._fetch_greenhouse_api_payload")
    def test_resume_variant_falls_back_to_default_for_non_technical_roles(
        self,
        mock_fetch_api: mock.Mock,
    ) -> None:
        payload = sample_policy_payload()
        payload["title"] = "Account Coordinator"
        payload["content"] = "Anthropic builds safe and reliable AI systems for enterprise use."
        mock_fetch_api.return_value = payload
        schema = load_greenhouse_application_schema(
            "https://job-boards.greenhouse.io/example/jobs/77777",
            session=object(),  # type: ignore[arg-type]
        )
        profile = {
            "resume_variants": {
                "ai": "/tmp/resume-ai.pdf",
                "distributed_systems": "/tmp/resume-distributed.pdf",
                "cloud": "/tmp/resume-cloud.pdf",
            },
            "resume_variant_default": "distributed_systems",
        }

        analysis = analyze_greenhouse_application(schema, profile=profile)
        autofill_targets = build_autofill_targets(analysis, profile=profile)
        autofill_values = {target.question_label: target.value for target in autofill_targets}

        self.assertEqual(autofill_values["Resume/CV"], "/tmp/resume-distributed.pdf")

    @mock.patch("career_monitor.greenhouse_assistant._fetch_greenhouse_api_payload")
    def test_target_company_employment_question_stays_review_only(
        self,
        mock_fetch_api: mock.Mock,
    ) -> None:
        mock_fetch_api.return_value = sample_target_company_employment_payload()
        schema = load_greenhouse_application_schema(
            "https://job-boards.greenhouse.io/stripe/jobs/12345",
            session=object(),  # type: ignore[arg-type]
        )
        profile = {
            "first_name": "Ada",
            "email": "ada@example.com",
            "resume_path": "/tmp/resume.pdf",
            "previously_employed_by_target_company": False,
        }
        analysis = analyze_greenhouse_application(schema, profile=profile)
        approved_targets = build_approved_answer_targets(
            analysis,
            approved_answers=None,
            profile=profile,
        )

        actions = {question.label: question.decision.action for question in analysis.schema.questions}
        statuses = {item.question_label: item.status for item in analysis.review_queue}
        self.assertEqual(
            actions["Have you ever been employed by Stripe or a Stripe affiliate?"],
            "REVIEW",
        )
        self.assertEqual(
            statuses["Have you ever been employed by Stripe or a Stripe affiliate?"],
            "pending",
        )
        approved_labels = {target.question_label for target in approved_targets}
        self.assertNotIn("Have you ever been employed by Stripe or a Stripe affiliate?", approved_labels)
        self.assertFalse(analysis.auto_submit_eligible)

    @mock.patch("career_monitor.greenhouse_assistant._fetch_greenhouse_api_payload")
    def test_build_approved_answer_targets_supports_checkbox_inputs(
        self,
        mock_fetch_api: mock.Mock,
    ) -> None:
        mock_fetch_api.return_value = sample_taxonomy_payload()
        schema = load_greenhouse_application_schema(
            "https://job-boards.greenhouse.io/example/jobs/99999",
            session=object(),  # type: ignore[arg-type]
        )
        analysis = analyze_greenhouse_application(schema, profile={})
        approved_targets = build_approved_answer_targets(
            analysis,
            approved_answers={
                "Please review and acknowledge Cloudflare's Candidate Privacy Policy (cloudflare.com/candidate-privacy-notice/).": True,
            },
            profile={},
        )

        target_types = {(target.question_label, target.ui_type, target.value) for target in approved_targets}
        self.assertIn(
            (
                "Please review and acknowledge Cloudflare's Candidate Privacy Policy (cloudflare.com/candidate-privacy-notice/).",
                "checkbox_group",
                "Yes",
            ),
            target_types,
        )

    @mock.patch("career_monitor.greenhouse_assistant._fetch_greenhouse_api_payload")
    def test_build_approved_answer_targets_supports_data_compliance_checkbox(
        self,
        mock_fetch_api: mock.Mock,
    ) -> None:
        mock_fetch_api.return_value = sample_api_payload(with_consent=True)
        schema = load_greenhouse_application_schema(
            "https://job-boards.greenhouse.io/example/jobs/12345",
            session=object(),  # type: ignore[arg-type]
        )
        analysis = analyze_greenhouse_application(schema, profile={})
        approved_targets = build_approved_answer_targets(
            analysis,
            approved_answers={"Consent to processing of personal data": True},
            profile={},
        )

        target_types = {(target.question_label, target.ui_type, target.value) for target in approved_targets}
        self.assertIn(
            ("Consent to processing of personal data", "checkbox", "Yes"),
            target_types,
        )

    @mock.patch("career_monitor.greenhouse_assistant._fetch_greenhouse_api_payload")
    def test_build_approved_answer_targets_supports_cover_letter_file_upload(
        self,
        mock_fetch_api: mock.Mock,
    ) -> None:
        mock_fetch_api.return_value = sample_cover_letter_payload()
        schema = load_greenhouse_application_schema(
            "https://job-boards.greenhouse.io/example/jobs/22222",
            session=object(),  # type: ignore[arg-type]
        )
        analysis = analyze_greenhouse_application(
            schema,
            profile={"first_name": "Ada"},
        )
        approved_targets = build_approved_answer_targets(
            analysis,
            approved_answers={"Cover Letter": "/tmp/cover-letter.pdf"},
            profile={},
        )

        target_types = {(target.question_label, target.ui_type, target.value) for target in approved_targets}
        self.assertIn(("Cover Letter", "file_upload", "/tmp/cover-letter.pdf"), target_types)

    @mock.patch("career_monitor.greenhouse_assistant._fetch_greenhouse_api_payload")
    def test_optional_cover_letter_stays_in_review_queue(
        self,
        mock_fetch_api: mock.Mock,
    ) -> None:
        mock_fetch_api.return_value = sample_optional_cover_letter_payload()
        schema = load_greenhouse_application_schema(
            "https://job-boards.greenhouse.io/example/jobs/22222",
            session=object(),  # type: ignore[arg-type]
        )
        analysis = analyze_greenhouse_application(
            schema,
            profile={"first_name": "Ada"},
        )

        actions = {question.label: question.decision.action for question in analysis.schema.questions}
        statuses = {item.question_label: item.status for item in analysis.review_queue}
        self.assertEqual(actions["Cover Letter"], "REVIEW")
        self.assertEqual(statuses["Cover Letter"], "pending")
        self.assertFalse(analysis.auto_submit_eligible)

    @mock.patch("career_monitor.greenhouse_assistant._fetch_greenhouse_api_payload")
    def test_grafana_style_policy_answers_fill_deterministic_review_questions(
        self,
        mock_fetch_api: mock.Mock,
    ) -> None:
        mock_fetch_api.return_value = sample_grafana_payload()
        schema = load_greenhouse_application_schema(
            "https://job-boards.greenhouse.io/grafanalabs/jobs/5809023004",
            session=object(),  # type: ignore[arg-type]
        )
        profile = {
            "current_country": "United States",
            "location": "Boston, MA, United States",
            "default_authorized_to_work_answer": "Yes",
            "how_did_you_hear_about": "LinkedIn",
        }

        analysis = analyze_greenhouse_application(schema, profile=profile)
        approved_targets = build_approved_answer_targets(analysis, approved_answers=None, profile=profile)

        statuses = {item.question_label: item.status for item in analysis.review_queue}
        self.assertEqual(statuses["Are you located in and plan to work from the USA?"], "answered")
        self.assertEqual(
            statuses["Are you currently eligible to work in your country of residence?"],
            "answered",
        )
        self.assertEqual(statuses["Which of the following best describes you?"], "answered")
        self.assertEqual(statuses["How did you hear about this opportunity at Grafana?"], "answered")
        approved_values = {target.question_label: target.value for target in approved_targets}
        self.assertEqual(approved_values["Are you located in and plan to work from the USA?"], "Yes")
        self.assertEqual(
            approved_values["Are you currently eligible to work in your country of residence?"],
            "Yes",
        )
        self.assertEqual(
            approved_values["Which of the following best describes you?"],
            "I am a human being",
        )
        self.assertEqual(
            approved_values["How did you hear about this opportunity at Grafana?"],
            "LinkedIn",
        )

    @mock.patch("career_monitor.greenhouse_assistant._fetch_greenhouse_api_payload")
    def test_cloudflare_style_optional_legal_name_and_profile_link_autofill_from_profile(
        self,
        mock_fetch_api: mock.Mock,
    ) -> None:
        mock_fetch_api.return_value = sample_cloudflare_like_payload()
        schema = load_greenhouse_application_schema(
            "https://boards.greenhouse.io/cloudflare/jobs/7436794?gh_jid=7436794",
            session=object(),  # type: ignore[arg-type]
        )
        profile = {
            "first_name": "Sujendra",
            "last_name": "Gharat",
            "legal_first_name": "Sujendra Jayant",
            "legal_last_name": "Gharat",
            "linkedin": "https://www.linkedin.com/in/sujendra-gharat/",
            "website": "https://sujendra.netlify.app/",
        }

        analysis = analyze_greenhouse_application(schema, profile=profile)
        autofill_targets = build_autofill_targets(analysis, profile=profile)

        actions = {question.label: question.decision.action for question in analysis.schema.questions}
        self.assertEqual(actions["Legal Name (if different than above)"], "AUTOFILL")
        self.assertEqual(
            actions["Would you like to include your LinkedIn profile, personal website or blog?"],
            "AUTOFILL",
        )
        target_values = {target.question_label: target.value for target in autofill_targets}
        self.assertEqual(target_values["First Name"], "Sujendra")
        self.assertEqual(target_values["Legal Name (if different than above)"], "Sujendra Jayant Gharat")
        self.assertEqual(
            target_values["Would you like to include your LinkedIn profile, personal website or blog?"],
            "https://www.linkedin.com/in/sujendra-gharat/",
        )

    @mock.patch("career_monitor.greenhouse_assistant._fetch_greenhouse_api_payload")
    def test_first_name_uses_legal_name_when_preferred_name_field_exists(
        self,
        mock_fetch_api: mock.Mock,
    ) -> None:
        mock_fetch_api.return_value = sample_preferred_name_payload()
        schema = load_greenhouse_application_schema(
            "https://job-boards.greenhouse.io/example/jobs/12345",
            session=object(),  # type: ignore[arg-type]
        )
        profile = {
            "first_name": "Sujendra",
            "last_name": "Gharat",
            "preferred_first_name": "Sujendra",
            "legal_first_name": "Sujendra Jayant",
            "legal_last_name": "Gharat",
        }

        analysis = analyze_greenhouse_application(schema, profile=profile)
        autofill_targets = build_autofill_targets(analysis, profile=profile)
        target_values = {target.question_label: target.value for target in autofill_targets}

        self.assertEqual(target_values["First Name"], "Sujendra Jayant")
        self.assertEqual(target_values["Preferred First Name"], "Sujendra")
        self.assertEqual(target_values["Last Name"], "Gharat")

    @mock.patch("career_monitor.greenhouse_assistant._fetch_greenhouse_api_payload")
    def test_anthropic_style_policy_questions_are_prefilled_for_headed_review(
        self,
        mock_fetch_api: mock.Mock,
    ) -> None:
        mock_fetch_api.return_value = sample_anthropic_like_payload()
        schema = load_greenhouse_application_schema(
            "https://job-boards.greenhouse.io/anthropic/jobs/5098984008",
            session=object(),  # type: ignore[arg-type]
        )
        profile = {
            "open_to_hybrid_or_onsite": True,
            "requires_visa_sponsorship_now": False,
            "default_earliest_start_answer": "Within 2 weeks of offer acceptance.",
            "default_timeline_considerations_answer": "No immediate deadlines. I can coordinate timing based on the interview process and my current commitments.",
            "current_work_address": "Boston, Massachusetts, 02215",
            "interviewed_at_target_company_before": False,
            "default_demographic_decline": True,
            "gender_answer": "Decline To Self Identify",
            "hispanic_latino_answer": "No",
            "race_answer": "Asian",
            "veteran_status": "I am not a protected veteran",
            "company_specific_answers": {
                "anthropic": {
                    "Why Anthropic?": "Anthropic stands out to me because of its focus on building reliable, useful AI systems with strong engineering discipline. I am motivated by roles where product thinking, infrastructure, and practical AI work come together, and this role sits directly at that intersection.",
                    "Additional Information": "I bring a mix of software engineering, distributed systems, cloud infrastructure, and applied AI interest.",
                }
            },
        }
        analysis = analyze_greenhouse_application(schema, profile=profile)
        approved_targets = build_approved_answer_targets(analysis, approved_answers=None, profile=profile)
        statuses = {item.question_label: item.status for item in analysis.review_queue}
        approved_values = {target.question_label: target.value for target in approved_targets}

        self.assertEqual(
            statuses["Are you open to working in-person in one of our offices 25% of the time?"],
            "answered",
        )
        self.assertEqual(statuses["AI Policy for Application"], "answered")
        self.assertEqual(statuses["Why Anthropic?"], "answered")
        self.assertEqual(statuses["Additional Information"], "answered")
        self.assertEqual(statuses["Have you ever interviewed at Anthropic before?"], "answered")
        self.assertEqual(
            approved_values["Are you open to working in-person in one of our offices 25% of the time?"],
            "Yes",
        )
        self.assertEqual(approved_values["AI Policy for Application"], "Yes")
        self.assertEqual(
            approved_values["When is the earliest you would want to start working with us?"],
            "Within 2 weeks of offer acceptance.",
        )
        self.assertEqual(
            approved_values["Do you have any deadlines or timeline considerations we should be aware of?"],
            "No immediate deadlines. I can coordinate timing based on the interview process and my current commitments.",
        )
        self.assertEqual(
            approved_values["What is the address from which you plan on working? If you would need to relocate, please type \"relocating\"."],
            "Boston, Massachusetts, 02215",
        )
        self.assertEqual(approved_values["Have you ever interviewed at Anthropic before?"], "No")
        self.assertEqual(approved_values["Are you Hispanic/Latino?"], "No")
        self.assertEqual(approved_values["Gender"], "Decline To Self Identify")
        self.assertEqual(approved_values["Race"], "Asian")
        self.assertEqual(approved_values["Veteran Status"], "I am not a protected veteran")

    @mock.patch("career_monitor.greenhouse_assistant._call_greenhouse_slm")
    @mock.patch("career_monitor.greenhouse_assistant._fetch_greenhouse_api_payload")
    def test_interactive_suggested_targets_can_prefill_queue_items(
        self,
        mock_fetch_api: mock.Mock,
        mock_call_slm: mock.Mock,
    ) -> None:
        mock_fetch_api.return_value = sample_anthropic_like_payload()
        schema = load_greenhouse_application_schema(
            "https://job-boards.greenhouse.io/anthropic/jobs/5098984008",
            session=object(),  # type: ignore[arg-type]
        )
        analysis = analyze_greenhouse_application(schema, profile={})

        def _fake_suggestion(*, question, **_kwargs):
            if question.label == "Why Anthropic?":
                return GreenhouseSuggestedAnswer(
                    question_label=question.label,
                    api_name="question_why",
                    value="I want to work at Anthropic because the mission and engineering challenges around reliable AI systems match my background and interests.",
                    source="slm",
                    reason="Drafted from role and company context.",
                    confidence=0.72,
                )
            return None

        mock_call_slm.side_effect = _fake_suggestion
        targets = build_interactive_suggested_answer_targets(analysis, profile={})
        target_values = {target.question_label: target.value_source for target in targets}

        self.assertEqual(target_values["Why Anthropic?"], "slm_interactive_suggestion")

    @mock.patch("career_monitor.greenhouse_assistant._call_greenhouse_slm")
    @mock.patch("career_monitor.greenhouse_assistant._greenhouse_slm_enabled")
    @mock.patch("career_monitor.greenhouse_assistant._fetch_greenhouse_api_payload")
    def test_slm_suggestions_prefill_custom_review_questions_but_keep_review_pending(
        self,
        mock_fetch_api: mock.Mock,
        mock_slm_enabled: mock.Mock,
        mock_call_greenhouse_slm: mock.Mock,
    ) -> None:
        mock_fetch_api.return_value = sample_grafana_payload()
        mock_slm_enabled.return_value = True
        mock_call_greenhouse_slm.return_value = mock.Mock(
            question_label="Do you have experience with provisioning Kubernetes clusters and operating in production?",
            api_name="question_k8s_provisioning",
            value="Yes - I have production Kubernetes provisioning experience.",
            source="slm",
            reason="Profile and role context both indicate cloud/platform experience.",
            confidence=0.82,
        )

        schema = load_greenhouse_application_schema(
            "https://job-boards.greenhouse.io/grafanalabs/jobs/5809023004",
            session=object(),  # type: ignore[arg-type]
        )
        analysis = analyze_greenhouse_application(
            schema,
            profile={"current_country": "United States", "default_authorized_to_work_answer": "Yes"},
        )
        suggested_targets = build_suggested_answer_targets(analysis)

        review_items = {
            item.question_label: item
            for item in analysis.review_queue
        }
        provisioning_item = review_items[
            "Do you have experience with provisioning Kubernetes clusters and operating in production?"
        ]
        self.assertEqual(provisioning_item.status, "pending")
        self.assertEqual(
            provisioning_item.suggested_answer,
            "Yes - I have production Kubernetes provisioning experience.",
        )
        self.assertEqual(provisioning_item.suggested_answer_source, "slm")
        self.assertFalse(analysis.auto_submit_eligible)
        self.assertEqual(len(suggested_targets), 1)
        self.assertEqual(suggested_targets[0].value_source, "slm_suggestion")
        self.assertEqual(
            suggested_targets[0].value,
            "Yes - I have production Kubernetes provisioning experience.",
        )

    @mock.patch("career_monitor.greenhouse_assistant.urllib.request.urlopen")
    @mock.patch("career_monitor.greenhouse_assistant.retrieve_profile_knowledge")
    @mock.patch("career_monitor.greenhouse_assistant._greenhouse_slm_enabled")
    @mock.patch("career_monitor.greenhouse_assistant._fetch_greenhouse_api_payload")
    def test_profile_knowledge_direct_suggestion_prefills_experience_text_question_without_slm_call(
        self,
        mock_fetch_api: mock.Mock,
        mock_slm_enabled: mock.Mock,
        mock_retrieve: mock.Mock,
        mock_urlopen: mock.Mock,
    ) -> None:
        mock_fetch_api.return_value = sample_grafana_payload()
        mock_slm_enabled.return_value = True
        mock_retrieve.return_value = ProfileRetrievalResult(
            evidence_chunks=[
                ProfileRetrievalItem(
                    item_id="pkc_infra",
                    source_file="gcp-infra.md",
                    section_title="Cloud Infra Project Summary > What Was Implemented",
                    text="Implemented Terraform-managed cloud infrastructure and deployment workflows with Kubernetes-oriented operational concerns and infrastructure validation.",
                    word_count=15,
                    topic_tags=["cloud", "infra", "kubernetes"],
                    score=7.4,
                    reasons=["tags:cloud, infra, kubernetes"],
                )
            ],
            style_snippets=[],
            retrieval_summary=["matched tags: cloud, infra, kubernetes"],
            matched_tags=["cloud", "infra", "kubernetes"],
            has_strong_evidence=True,
        )
        schema = load_greenhouse_application_schema(
            "https://job-boards.greenhouse.io/grafanalabs/jobs/5809023004",
            session=object(),  # type: ignore[arg-type]
        )
        question = next(
            item
            for item in schema.questions
            if item.label == "Do you have experience with provisioning Kubernetes clusters and operating in production?"
        )

        suggestion = _call_greenhouse_slm(
            schema=schema,
            question=question,
            profile={"selected_resume_variant": "cloud"},
        )

        self.assertIsNotNone(suggestion)
        assert suggestion is not None
        self.assertTrue(suggestion.value.startswith("Yes — Implemented Terraform-managed cloud infrastructure"))
        self.assertEqual(suggestion.source, "profile_knowledge")
        self.assertEqual(suggestion.draft_source, "profile_knowledge")
        self.assertEqual(suggestion.retrieved_chunk_ids, ["pkc_infra"])
        mock_urlopen.assert_not_called()


if __name__ == "__main__":
    unittest.main()
