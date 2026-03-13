# Greenhouse Form Survey Summary

Generated from `.state/greenhouse_form_survey.json` on 2026-03-07.

## Coverage

- Configured Greenhouse boards surveyed: 11
- Jobs discovered and surveyed: 2,961
- Job-level schema failures: 0
- Unique question labels observed: 1,559
- Unique normalized question labels observed: 1,524
- Unique field signatures observed: 9

## Core Form Shape

Across Greenhouse-hosted and company-hosted Greenhouse application flows, the dominant recurring fields were:

- `First Name`
- `Last Name`
- `Email`
- `Phone`
- `Resume/CV`
- `Cover Letter`
- `Location`
- `Latitude`
- `Longitude`
- `LinkedIn Profile`
- `Website`

Average questions per job:

- total questions: 19.83
- required questions: 13.49

Observed question groups:

- `questions`: 44,871
- `location_questions`: 6,039
- `compliance`: 5,608
- `demographic_questions`: 2,156
- `data_compliance`: 51

## Field Signatures

The entire survey reduced to 9 distinct input-shape signatures:

1. `text_input:input_text`
2. `combobox:multi_value_single_select`
3. `file_upload:input_file + textarea:textarea`
4. `hidden:input_hidden`
5. `unknown`
6. `checkbox_group:multi_value_multi_select`
7. `textarea:textarea`
8. `checkbox:consent`
9. `file_upload:input_file`

This means the form surface is broad in wording, but still narrow in actual widget types.

## Most Common Questions

The most common normalized question families were:

1. `email`
2. `first name`
3. `last name`
4. `phone`
5. `resume cv`
6. `cover letter`
7. `latitude`
8. `longitude`
9. `location`
10. `linkedin profile`
11. `race`
12. `gender`
13. `disabilitystatus`
14. `veteranstatus`
15. `website`

## Notable Company-Specific Patterns

### Stripe

- Large use of required `location_questions`
- frequent work authorization and sponsorship questions
- WhatsApp opt-in question present in many jobs
- prior employment at Stripe question appears often

### Cloudflare

- candidate privacy policy acknowledgement appears as a required checkbox-group field
- legal-name question is common
- many roles ask how the candidate heard about the job
- immigration sponsorship question is frequent and required

### Anthropic

- `AI Policy for Application` appears frequently and is required
- `Why Anthropic?` is a high-frequency required open-ended question

### Elastic

- candidate privacy acknowledgement is common and frequently required
- sponsorship question is widespread

### MongoDB and Dropbox

- many applications request `LinkedIn Profile`
- legal-name variants appear in Dropbox

### Grafana Labs and Temporal

- `Preferred First Name` is common
- sponsorship questions are common and often required

### New Relic

- country-of-residence and state fields are consistently present

### Box

- prior-employment history at Box is common

## Implications For The Assistant

- deterministic autofill should focus on the recurring structured set: name, email, phone, resume, LinkedIn, website, location
- required location flows need support for `Location`, `Latitude`, and `Longitude`
- review logic must handle sponsorship, work authorization, prior-employment, privacy acknowledgement, and consent-style questions
- queue logic must expect required open-ended prompts such as `Why Anthropic?`
- checkbox-group support matters for policy acknowledgements and multi-country selection
- demographic/compliance handling must remain isolated from autonomous submit decisions

## Artifacts

- Full machine-readable report: `.state/greenhouse_form_survey.json`
- Survey implementation: `career_monitor/greenhouse_survey.py`
