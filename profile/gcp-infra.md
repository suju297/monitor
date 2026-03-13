# Cloud Infra Project Summary

## Project

This project is composed of three repositories:

- `webapp-fork`: Node.js REST API for user management
- `serverless-fork`: Pub/Sub-triggered Cloud Function for email verification
- `tf-gcp-infra-fork`: Terraform stack for GCP infrastructure

## End-to-End Architecture

1. A user signs up through the Node.js API.
2. The API stores user data in MySQL or Cloud SQL using Sequelize.
3. The API generates a verification token and verification link.
4. In cloud mode, the API publishes a verification event to a Pub/Sub topic.
5. A Cloud Function consumes the Pub/Sub event.
6. The Cloud Function updates the verification expiry in Cloud SQL and sends the verification email through Mailgun.
7. The web tier runs on Compute Engine instances behind an HTTPS load balancer with autoscaling.
8. Terraform provisions the VPC, subnets, Cloud SQL, Pub/Sub, Cloud Function, service accounts, DNS, SSL, KMS, autoscaler, and load balancer.

## What Was Implemented

### Web Application

- Fixed the application start flow so the server initializes the database before listening.
- Added a real local development setup with Docker and MySQL.
- Added `.env.example`, `Dockerfile`, and `docker-compose.yml`.
- Improved the signup and verification flow so local development works without Pub/Sub and cloud deployment works with Pub/Sub.
- Made logging compatible with local execution and Google Ops Agent ingestion.
- Added packaging support for Packer with `npm run package:zip`.

### Serverless Function

- Removed the hardcoded Mailgun API key from source code.
- Switched Mailgun configuration to environment variables.
- Added local start support with Functions Framework.
- Added packaging support for Terraform with `npm run package:zip`.
- Kept the function aligned with the user-verification flow from the API.

### Terraform Infrastructure

- Added explicit Terraform provider declarations for `google`, `google-beta`, and `random`.
- Corrected region and DNS handling in the stack.
- Added outputs for load balancer IP, DNS record, Cloud SQL private IP, Pub/Sub topic, and Cloud Function name.
- Added `terraform.tfvars.example` so the stack can be filled and applied more easily.
- Updated startup configuration so the VM-based web tier receives the environment it needs.

## Local Validation

The API was validated locally with Docker:

- `GET /healthz` returned `200`
- `POST /v2/user` returned `201`
- `GET /verify?token=...` returned `200`
- `GET /v2/user/self` returned `200`

The existing integration test suite also passed:

- `4/4` tests passed for the web application

The serverless function was also dependency-checked and loaded successfully.

## Local Run

### Web App

```bash
cd webapp-fork
docker compose up --build
```

API base URL:

```bash
http://localhost:8080
```

### Serverless Function

```bash
cd serverless-fork
npm install
npm start
```

Function base URL:

```bash
http://localhost:8081
```

## Packaging and Deployment Flow

### Package Web App for Packer

```bash
cd webapp-fork
npm install
npm run package:zip
packer init packer/packer.pkr.hcl
packer build packer/packer.pkr.hcl
```

### Package Cloud Function

```bash
cd serverless-fork
npm install
npm run package:zip
```

### Deploy Infrastructure

```bash
cd tf-gcp-infra-fork
cp terraform.tfvars.example terraform.tfvars
terraform init
terraform fmt
terraform validate
terraform plan
terraform apply
```

## Current Limitation

Terraform was not executed in this environment because the `terraform` CLI is not installed locally here. The Terraform code was updated and documented, but it still needs a real `terraform validate` and `terraform plan` run on a machine with Terraform installed and valid GCP credentials.

## Security Note

`webapp-fork/packer/packer-svc.json` appears to be a service account credential file. It was excluded from packaged artifacts, but if it is a real credential, it should be rotated and removed from Git history.

## Resume Points

- Built a GCP-based cloud application platform using Node.js, MySQL/Cloud SQL, Pub/Sub, Cloud Functions, Compute Engine, HTTPS load balancing, Cloud DNS, KMS, and Terraform.
- Implemented an asynchronous email-verification pipeline that published signup events to Pub/Sub, processed them in a Cloud Function, updated Cloud SQL state, and sent verification emails through Mailgun.
- Automated infrastructure provisioning for networking, encrypted storage, private database connectivity, autoscaling compute, and DNS-backed HTTPS delivery on Google Cloud.
- Added Docker-based local development, integration testing, structured logging, and packaging workflows for both VM image creation with Packer and Cloud Function deployment artifacts.

## Interview Summary

This project demonstrates full-stack cloud engineering across application development, async backend workflows, infrastructure as code, observability, deployment automation, and secure GCP resource design.