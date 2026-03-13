# SUJENDRA JAYANT GHARAT

Boston, MA  
Email | LinkedIn | GitHub

---

## PROFESSIONAL SUMMARY

Full stack and AI engineer with experience building production-grade systems across biotech, healthcare, and applied AI research. Proven ability to design, ship, and operate end-to-end systems spanning frontend, backend, cloud infrastructure, and ML/LLM pipelines. Experienced in handling terabyte-scale scientific data, integrating ML into real-world workflows, and improving AI system quality through evaluation-driven redesign. Strong focus on reliability, safety, and usability for non-technical users.

---

## CORE SKILLS

**Languages:** Python, TypeScript, JavaScript, SQL  
**Backend:** FastAPI, Django, Flask, Node.js, Express, Redis  
**Frontend:** Next.js, React, Angular, React Native, 3.js  
**Cloud & Infra:** AWS S3, Docker, Kubernetes, Helm, PM2  
**AI & Data:** LangChain, LLM pipelines, evaluation, TensorFlow Serving, Plotly, statistical testing  
**Healthcare & Standards:** HL7 FHIR, medical imaging (DICOM, MRI, ultrasound)

---

## EXPERIENCE

### XELLAR BIOSYSTEMS — Software Intern

Boston, MA | Jan 13, 2025 – Aug 15, 2025

Worked on the core organ-chip experimentation platform across full stack development, cloud infrastructure, and scientific data processing.

- Built an **Experiment Designer** for **8-chip and 32-chip OC Plex devices**, implementing workflows for drug assignment, dose concentration mapping, and chip configuration.
    
- Designed a **template system** for reusable dosing patterns, enabling rapid setup of recurring experiments and reducing manual configuration errors.
    
- Introduced **row-based dosing**, **serial dilution patterns**, and **automatic chip mapping** for the 32-chip device, replacing a rigid flow that required manual per-chip edits.
    
- Identified risk of silent data loss due to missing S3 bucket versioning and built a **reliable backup system** for microscopy datasets.
    
- Each experiment generated **200–300 GB+**, and due to historical gaps in backups, supported recovery and backup of **terabytes of accumulated data across multiple experiments**.
    
- Implemented **safe write policies** (overwrite, rename-on-collision, skip-existing) and productized the workflow from a script into a **FastAPI, Redis, Docker, and Next.js** application.
    
- Exposed storage tier selection (including cold storage), retries, failure recovery, and logging in the UI so **non-technical biologists** could manage backups without AWS CLI usage.
    
- Built a **cloud-based microscopy image pipeline** for **16-bit multi-channel TIFF images (typically 4 channels)**.
    
- Implemented **8-bit conversion** using min–max and percentile normalization with per-channel processing to preserve image sensitivity.
    
- Added caching and metadata-driven retrieval, reducing average image access latency from **~1.0s to ~0.4s** at experiment scale.
    
- Developed an **interactive image viewer** with pan, zoom, and region-based zoom, integrated directly into the experiment platform.
    
- Contributed to the **data analysis module**, building visual analytics in Plotly including bar charts, line charts, and dose–response curves.
    
- Implemented **pairwise comparisons and Dunnett-style post hoc tests**, displaying p-values and significance markers directly on plots to connect experimental design, imaging, and statistical interpretation.
    

---

### NORTHEASTERN UNIVERSITY — AI-CARING INSTITUTE

**Graduate Research Assistant**  
Boston, MA | Sep 2023 – Present

Worked on an ambient reminder system for individuals with mild cognitive impairment, combining natural language dialogue, in-home sensors, and human activity recognition.

- Built the **end-to-end system**, including a **Next.js frontend**, **FastAPI backend**, and a **LangChain-based LLM pipeline**, deployed on an in-house server.
    
- Implemented pipelines for reminder authoring, intent extraction, sensor and activity mapping, feasibility checking, and trigger code generation.
    

**Study 1:**

- Designed a **multi-stage pipeline** for dialogue, intent extraction, feasibility, and trigger generation using GPT-4o.
    
- Identified key failure modes due to unmodeled **edge cases and constraints**, including ambiguous time expressions, routines, sequential logic, and unsupported sensor–activity combinations.
    
- Many outputs were syntactically valid but failed to behave correctly in real-world settings.
    

**Study 2 Redesign:**

- Led redesign based on evaluation findings, separating conversational clarification from structured intent extraction.
    
- Introduced **event-based abstractions grounded in detectable sensor capabilities**.
    
- Moved toward **code generation for trigger logic** to support realistic combinations of time constraints, sensor conditions, activities, and sequences.
    
- Mapped sensors to **spatial locations** and implemented a **feasibility gate** using a house graph and sensor constraints to prevent impossible reminders.
    

**Results:**

- **Correct reminders:** 38.6% → 88.3%
    
- **Incorrect reminders:** 43.3% → 0.0%
    
- **Acceptable reminders (Correct + Contextually Reasonable):** 95.0%
    
- Supported **two user studies on Prolific** and contributed to a paper submission.
    

---

### CAPGEMINI — Software Engineer

India | Feb 7, 2022 – Aug 15, 2023  
**Clients: GE Healthcare, Multimodality AI**

- Built **Flask APIs** connecting an **Angular medical imaging UI** to **Dockerized TensorFlow Serving**.
    
- Supported imaging modalities including **DICOM, MRI, and ultrasound**, routing inputs to appropriate models.
    
- Rendered inference results as **coordinate-based overlays and masks** on images and enabled annotation workflows using **Cornerstone**.
    
- Supported use cases including lung imaging for COVID-related detection and tumor detection with **3D visualization** needs.
    
- Contributed to a **Multimodality AI (MMAI)** platform presenting longitudinal patient histories as timelines for clinical interpretation.
    
- Built backend services using **FastAPI initially and later Django**, integrated with Angular frontend components.
    
- Designed workflows using **Edison Orchestrator**, implemented **HL7 FHIR**-compliant data ingestion and cleaning pipelines.
    
- Containerized services with **Docker** and deployed on **Kubernetes using Helm charts**; work was presented at **RSNA**.
    
- Built **cross-platform installation scripts** (Windows and Linux) to validate prerequisites, start containers, and guide users to the web interface.
    
- Worked on a **3D brain MRI visualization web app**, enabling interactive rotation with controlled axes and degrees using Imaging Fabric.
    

---

### LTIMINDTREE — Software Engineer

India | Aug 16, 2018 – Feb 2, 2022

- Built and scaled backend services for a consumer electricity platform serving **~60 lakh (6 million) users** across Uttar Pradesh and Haryana.
    
- Developed **Node.js and Express APIs**, integrating billing, complaint management, SOA middleware, and **Meter Data Management (MDM)** systems.
    
- Implemented **sequential and parallel workflows** using Promise-based concurrency patterns.
    
- Delivered electricity consumption analytics across **hourly, daily, weekly, monthly, yearly, and on-demand** views.
    
- Contributed to a **React Native mobile application** offering the same functionality.
    
- Built **billing calculators and estimators** using standard utility formulas and supported end-to-end bill generation.
    
- Implemented **MongoDB-based caching** to mitigate slow upstream queries and daily MDM refresh cycles.
    
- Managed Node.js processes using **PM2** and supported cloud deployment.
    
- Represented engineering in integration discussions with external stakeholders.