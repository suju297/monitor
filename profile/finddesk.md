# FinDesk Copilot Resume Notes

## Project summary

FinDesk Copilot is a hackathon-style financial analysis prototype that combines:

- a Flask backend API
- FinRobot/AutoGen agent workflows
- a Next.js trading-desk UI
- experimental data pipelines for scraping, retrieval, and streaming market data

At a product level, the app lets a user request market analysis for a company, start a risk-analysis session, and ask follow-up questions in the same session. The frontend presents those workflows in a desk-style interface with role-based views, charts, option panels, and watchlist-driven navigation.

## What this project is doing

### Core app flow

1. The frontend sends a company name to the Flask API.
2. The Flask API starts either:
   - a one-shot market-analysis flow, or
   - a stateful risk-analysis session with follow-up questions
3. FinRobot/AutoGen agents call finance data connectors such as Finnhub and Yahoo Finance.
4. The backend returns analysis text to the frontend for display in the desk UI.

### Product pieces in the repo

- `api/`: Flask routes for market analysis, risk-analysis sessions, health checks, and macro event scraping.
- `stockbot/`: Next.js frontend with chat surfaces, role-based trading views, positions panels, and market widgets.
- `FinRobot/`: embedded upstream agent framework and financial data/tooling code used by the backend.
- `options_data_streaming/`: Kafka/MSK producer prototype for options pricing events.
- `modules/db/`: AWS Timestream ingestion prototype for market quote data.
- `datasets/`: notebook/data prep work.

### Experimental extensions

- Quant StackExchange scraper that collects answered quant questions into CSV.
- LlamaIndex-based retrieval prototype over scraped quant content.
- Economic calendar scraper for external macro-event context.
- Polygon client utilities.
- Kafka/MSK and Timestream scripts for streaming/warehouse experiments.

## Tech stack

- Python, Flask, Flask-CORS
- AutoGen, FinRobot
- Finnhub, Yahoo Finance, Polygon, FXStreet
- Next.js 14, React, TypeScript, Tailwind CSS, Radix UI
- Recharts, TradingView widgets
- Pandas, BeautifulSoup, LlamaIndex, OpenAI embeddings
- AWS MSK / Kafka, AWS Timestream, boto3

## Resume bullets

### Strong, safe bullets

- Built a Flask-based financial copilot API that wrapped FinRobot/AutoGen agent workflows and exposed market-analysis and session-based risk-analysis endpoints for company research.
- Implemented a stateful risk-analysis flow that preserved conversation history across follow-up questions, enabling iterative company risk reviews instead of one-shot responses.
- Integrated finance data connectors and utilities across Finnhub, Yahoo Finance, Polygon, and macro-event scraping to enrich agent outputs with company, market, and economic context.
- Developed a Next.js trading-desk interface with role-based views, positions panels, watchlists, and interactive market widgets for exploring stocks, options, and agent outputs.
- Prototyped retrieval and data-platform extensions by scraping Quant StackExchange content, building a LlamaIndex-based knowledge query workflow, and testing Kafka/MSK plus AWS Timestream ingestion for market data.

### Shorter version for a one-project resume entry

- Built a Flask + FinRobot financial copilot that generated market and risk analysis using LLM agents, finance data APIs, and stateful follow-up Q&A.
- Developed a Next.js trading-desk UI with role-based workflows, watchlists, positions, and interactive stock/options views tied to backend analysis services.
- Extended the prototype with quant-data scraping, retrieval experiments, and AWS streaming pipelines using Kafka/MSK and Timestream.

## How to describe your ownership honestly

Use language like:

- "built and integrated"
- "wrapped FinRobot workflows in a Flask API"
- "developed the app layer on top of FinRobot and StockBot-style components"
- "prototyped"

Avoid saying:

- "built FinRobot from scratch"
- "created the entire agent framework"
- "built a production trading platform"

This repo includes upstream/inherited code under `FinRobot/` and `stockbot/`, so the strongest truthful story is that you built the integration layer, product flow, and experiments around those components.

## Interview-ready explanation

FinDesk Copilot is an AI-assisted finance prototype. The main idea is to give a trading-desk style interface where a user can ask for market or risk analysis on a company, route that request through a Flask API, let FinRobot agents pull financial data from tools like Finnhub and Yahoo Finance, and return a synthesized answer back to the UI. Around that core flow, the repo also experiments with quant-content scraping, retrieval over that content, and streaming market-data ingestion through Kafka/MSK and AWS Timestream.

## Important repo caveats

- This is a prototype/hackathon-style repo, not a production-hardened platform.
- Some frontend paths reference research/risk endpoints that are commented out or only partially wired in the current backend.
- Several data-pipeline scripts depend on local credentials or cloud resources to run.
 # FinRobot Overview (Concise Explainer)

This document summarizes what FinRobot is, who it is for, and how it works, using
the project README as the primary source. Any gaps are explicitly marked as
"Not specified in README."

## What is FinRobot?
FinRobot is an open-source AI agent platform for financial applications. It
combines large language models (LLMs) with financial data sources and toolkits
to perform tasks like market analysis, report generation, and strategy research.

## Who is it for?
The README does not name a specific target user group. Based on the described
use cases and tutorials, the intended audience appears to be:
- Finance researchers and analysts
- Quants or strategy developers
- Engineers building financial AI workflows
- Students exploring agentic finance demos

## Target user categories (requested list)
- Researchers: Likely, based on research/reporting demos
- Quants: Likely, based on forecasting and strategy demos
- Retail traders: Possible, but not stated
- Risk teams: Mentioned via "Risk Analysis" use case in related code, but not
  emphasized in README
- Compliance teams: Not specified
- Students: Supported via tutorials

## Main jobs it does best (from README demos)
- Research reports (equity research report generation)
- Forecasting (stock movement direction prediction)
- Trading strategy agents (advanced tutorial)
- Other tasks (portfolio analysis, risk checks, backtesting, alerting) are not
  explicitly claimed in the README

## Input → Output flow (one concrete example)
Example: Market Forecaster Agent
1) Input: a company ticker, recent financials, and market news
2) Process: the agent retrieves data, reasons via Financial CoT, and summarizes
3) Output: a concise analysis of positives/concerns plus a rough direction
   forecast for next week

## Data and tools (what the agent can use)
FinRobot organizes tools in `finrobot/functional` and data connectors in
`finrobot/data_source`. These include:
- Data tools for financial APIs and filings
- Charting and quantitative utilities
- Report/PDF generation utilities
- Text processing utilities

## Data sources supported out of the box
From the module names listed in the README:
- Finnhub (market data/news) via `finnhub_utils.py`
- Financial Modeling Prep (fundamentals) via `fmp_utils.py`
- SEC filings via `sec_utils.py`
- Yahoo Finance via `yfinance_utils.py`
- FinNLP utilities via `finnlp_utils.py`

## Built-in connectors vs bring-your-own data
Built-in connectors are provided in `finrobot/data_source`. The architecture is
modular, so users can add their own data sources and tools if needed.

## Actions/tools agents can execute
Based on module names in `finrobot/functional`:
- Charting
- Quantitative analysis
- Report/PDF generation
- Text analysis
- General analysis/coding helpers

## Architecture details worth mentioning
The README describes four layers:
1) Financial AI Agents Layer
2) Financial LLMs Algorithms Layer
3) LLMOps and DataOps Layers
4) Multi-source LLM Foundation Models Layer

Core workflow: Perception → Brain (Financial CoT) → Action

## Repo modules (exact names)
From the README's file structure section:
- `finrobot/agents`
- `finrobot/data_source`
- `finrobot/functional`
- `finrobot/toolkits.py`
- `finrobot/utils.py`

## "Smart Scheduler" in plain terms
It selects and routes tasks to the most appropriate agent/model for the job,
based on agent availability and suitability. Components include:
- Director Agent: orchestrates task assignment
- Agent Registration: tracks agent availability
- Agent Adaptor: tailors agents to specific tasks
- Task Manager: stores and updates agents

## Single-agent or multi-agent?
The demos show single-agent classes like `SingleAssistant`, but the architecture
supports multiple agents coordinated by the Smart Scheduler.

## Differentiation (vs a generic agent repo)
FinRobot emphasizes:
- Financial Chain-of-Thought prompting
- Multi-agent scheduling and orchestration
- An end-to-end stack of tools, data sources, and workflows

## Key differentiator vs "LangChain + finance API"
The README highlights multi-agent orchestration and Financial CoT prompting,
not just a simple tool-calling chain. It does not claim eval harnesses,
guardrails, or reproducibility tooling.

## The one feature to brag about
The "Smart Scheduler" + Financial CoT workflow: routing tasks to the best agent
and structuring reasoning explicitly for financial analysis.

## Proof and credibility
- A published whitepaper: https://arxiv.org/abs/2405.14767
- PyPI download badges exist, but specific numbers are not provided in README

## Measurable results
Not specified in the README (no benchmarks, latency, or accuracy claims listed).

## Demos/notebooks (what they cover)
Tutorials:
- Beginner notebooks: market forecaster, annual report
- Advanced notebooks: trade strategist, forecasting, report generation,
  multimodal charting, strategy optimization

Demos highlighted:
- Market Forecaster Agent
- Financial Analyst Agent for report writing
- Trade Strategist Agent (mentioned)

## Adoption signals
Not specified in the README (no stars, forks, or contributor counts listed).

## What we built in this project (resume-ready)
If you are describing your personal work on this repo (not the upstream
FinRobot project), here is a concise summary based on the code in this
workspace:

- Built a Flask API layer that exposes FinRobot analysis endpoints, including
  market analysis and a stateful risk-analysis Q&A session flow.
- Integrated external market and macro sources via utility services:
  Polygon.io client, economic calendar scraper, and Quant StackExchange scraper
  with a scheduler.
- Prototyped a finance knowledge-graph pipeline using LlamaIndex and OpenAI
  embeddings.
- Implemented data-pipeline pieces for streaming market data:
  Kafka/MSK options producer and AWS Timestream ingestion.
- Included a separate interactive StockBot frontend for live charts and
  financial UI widgets.