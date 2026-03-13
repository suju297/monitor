# Greenhouse Candidate Intake

Use this to provide the personal information needed to expand autonomous Greenhouse handling beyond basic autofill.

You can answer in either format:

- reply in chat section by section
- fill the JSON template at the bottom and send it back

## 1. Core Identity

Required:

- `first_name`
- `last_name`
- `email`
- `phone`
- `location`

Optional but useful:

- `preferred_first_name`
- `preferred_name`
- `full_name`
- `legal_first_name`
- `legal_last_name`

Questions:

1. What is your first name?
2. What is your last name?
3. What email should be used for applications?
4. What phone number should be used?
5. What location should be used on applications?
6. Do you want a preferred first name or preferred name used when asked?
7. If a company asks for your legal first or last name, what should we use?

## 2. Links And Files

Questions:

1. What is your LinkedIn profile URL?
2. What is your GitHub profile URL, if any?
3. What website or portfolio URL should we use, if any?
4. What local file path should we use for your resume?
5. What local file path should we use for your cover letter, if any?

## 3. Work Authorization

These answers are needed for the most common review-sensitive Greenhouse questions.

Questions:

1. What country do you currently reside in?
2. What state or region do you currently reside in, if relevant?
3. Are you currently authorized to work in the United States?
4. Do you require visa sponsorship now?
5. Will you require visa sponsorship in the future?
6. If a form asks whether you are authorized to work in the job location, should we answer `yes` or `no` by default?
7. If a form asks whether you require immigration or employment sponsorship, should we answer `yes` or `no` by default?

## 4. Location And Work Style

Questions:

1. Are you open to relocation?
2. Are you open to hybrid or in-office roles?
3. If a form asks whether you plan to work remotely when remote is available, should we answer `yes` or `no`?
4. If a form asks which country or countries you anticipate working in, what should we list?

## 5. Employment And Education

These showed up often enough in the survey to justify collecting once.

Questions:

1. What is your current employer?
2. What is your current or most recent job title?
3. Have you ever previously worked for a company you are applying to?
4. What is the most recent degree you obtained?
5. What is the most recent school you attended?

## 6. Communication And Consent Preferences

Questions:

1. If a company asks to opt in to recruiter WhatsApp messages, should we answer `yes` or `no` by default?
2. If a form asks you to acknowledge a candidate privacy policy, are you comfortable treating that as a default `acknowledge` answer when required?

## 7. Open-Ended Defaults

These will not be used for autonomous submission unless you explicitly approve them, but collecting them now helps.

Questions:

1. What short answer should we use when a form asks `Why do you want to work here?`
2. What short answer should we use when a form asks `Tell us about yourself` or similar?
3. What short answer should we use for `Additional Information`, if any?

## JSON Template

```json
{
  "first_name": "Sujendra Jayant",
  "last_name": "Gharat",
  "full_name": "Sujendra Jayant Gharat",
  "preferred_first_name": "Sujendra",
  "preferred_name": "Gharat",
  "legal_first_name": "Sujendra Jayant",
  "legal_last_name": "Gharat",
  "email": "gharat.su@northeastern.edu",
  "phone": "+1 8579301933",
  "location": "Boston, MA",
  "current_country": "United States",
  "current_state_or_region": "Massachusetts",
  "linkedin": "https://www.linkedin.com/in/sujendra-gharat/",
  "github": "https://github.com/suju297",
  "website": "https://sujendra.netlify.app/",
  "resume_path": "",
  "cover_letter_path": "",
  "authorized_to_work_in_us": "True",
  "requires_visa_sponsorship_now": "Not Needed Now",
  "requires_visa_sponsorship_future": "No",
  "default_authorized_to_work_answer": "Yes authorized to work in US",
  "default_requires_sponsorship_answer": "Not Needed",
  "open_to_relocation": "Yes",
  "open_to_hybrid_or_onsite": "Yes",
  "default_remote_work_answer": "Yes",
  "anticipated_work_countries": [],
  "current_employer": "Ipser Labs",
  "current_or_previous_job_title": "Software Engineer - II",
  "previously_employed_by_target_company": "Capgemini",
  "most_recent_degree": "Master of Science in Information Systems",
  "most_recent_school": "Northeastern University, Boston, MA",
  "whatsapp_opt_in_default": "Yes",
  "privacy_policy_ack_default": null,
  "default_why_company_answer": "Anthropic stands out to me because of its focus on building reliable and steerable AI systems while turning cutting-edge research into real products people use for serious work. I’m particularly excited by the Enterprise and Verticals team because it focuses on transforming complex professional workflows with AI rather than building standalone demos.

My experience building applied LLM systems has shown me how important it is to combine strong engineering with careful evaluation and reliability. At the AI-CARING Institute, I built a multi-stage LLM pipeline that converts natural language instructions into executable logic for a smart home reminder system. That work involved designing agent-like workflows, building backend services, and improving system accuracy through evaluation and iteration.

What excites me most about Anthropic is the opportunity to work at the intersection of research and product. The idea of collaborating with researchers to improve model capabilities and then shipping those improvements in real enterprise tools is extremely compelling. I’m motivated by building AI systems that people rely on every day, and Anthropic’s mission to create powerful, trustworthy AI aligns closely with the type of impact I want to have as an engineer.
",
  "default_tell_me_about_yourself_answer": "",
  "default_additional_information_answer": ""
}
```

## Notes

- The existing assistant already uses the basic keys such as `first_name`, `last_name`, `email`, `phone`, `location`, `linkedin`, `website`, `resume_path`, and `cover_letter_path`.
- The other keys in this intake are the next expansion set for review-sensitive and frequently repeated Greenhouse questions.
- If you answer in chat instead of JSON, I can convert your answers into a machine-ready profile object next.
