# Greenhouse Candidate Policy Template

Use this companion template for dynamic answers that should not be stored as one static string.

This is separate from `docs/greenhouse-candidate-intake.md` on purpose:

- the intake file is for stable profile data
- this file is for answer rules and per-company overrides

## Recommended Shape

```json
{
  "current_visa_status": "F-1 OPT",
  "authorized_to_work_in_us": true,
  "requires_visa_sponsorship_now": false,
  "requires_visa_sponsorship_future": false,
  "requires_sponsorship_if_opt_included": true,
  "default_authorized_to_work_answer": "Yes",
  "default_requires_sponsorship_answer": "No",
  "open_to_relocation": true,
  "open_to_hybrid_or_onsite": true,
  "default_remote_work_answer": "Yes",
  "current_country": "United States",
  "current_state_or_region": "Massachusetts",
  "current_employer": "Ipser Labs",
  "current_or_previous_job_title": "Software Engineer - II",
  "most_recent_degree": "Master of Science in Information Systems",
  "most_recent_school": "Northeastern University, Boston, MA",
  "previously_employed_by_target_company": false,
  "whatsapp_opt_in_default": true,
  "privacy_policy_ack_default": null,
  "resume_variant_default": "distributed_systems",
  "resume_variants": {
    "ai": "/Users/sujendragharat/Library/CloudStorage/GoogleDrive-sgharat298@gmail.com/My Drive/MacExternalCloud/Documents/Monitor/Resume/AI/Sujendra_Jayant_Gharat_Resume.pdf",
    "distributed_systems": "/Users/sujendragharat/Library/CloudStorage/GoogleDrive-sgharat298@gmail.com/My Drive/MacExternalCloud/Documents/Monitor/Resume/Distributed Systems/Sujendra_Jayant_Gharat_Resume.pdf",
    "cloud": "/Users/sujendragharat/Library/CloudStorage/GoogleDrive-sgharat298@gmail.com/My Drive/MacExternalCloud/Documents/Monitor/Resume/Cloud/Sujendra_Jayant_Gharat_Resume.pdf"
  },
  "company_specific_answers": {
    "anthropic": {
      "why_company": ""
    },
    "cloudflare": {
      "why_company": ""
    }
  }
}
```

## Important Rules

### Sponsorship

Use these rules:

- generic `Do you require sponsorship now or in the future?` -> `No`
- generic `Are you authorized to work in the US?` -> `Yes`
- `What is your current visa status?` -> `F-1 OPT`
- if the question explicitly says sponsorship includes `F-1 OPT` or `STEM OPT` -> `Yes`

That is exactly what the current assistant logic now supports.

### Resume Selection

The assistant now supports `resume_variants` and will choose a variant from:

- `ai`
- `distributed_systems`
- `cloud`

Behavior:

- for technical AI-heavy roles, prefer `ai`
- for cloud / infra / devops roles, prefer `cloud`
- otherwise fall back to `resume_variant_default`

Non-technical roles should not get auto-routed to the AI resume just because the company works on AI.

### Why-Company Answers

Do not rely on one global `default_why_company_answer`.

Better options:

- leave open-ended `why company` questions in the queue
- or maintain `company_specific_answers`

This avoids reusing an Anthropic-specific answer on unrelated companies.

## Notes

- `previously_employed_by_target_company` should be a boolean, not the name of another employer.
- `default_requires_sponsorship_answer` should stay short, because many Greenhouse review fields are `Yes`/`No` comboboxes.
- If you want, the next step is to convert your filled intake answers plus these policy rules into a single machine-ready profile JSON for the assistant.
