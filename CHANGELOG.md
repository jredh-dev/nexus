# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- **services/cal**: iCal calendar subscription service
  - Standalone HTTP server with embedded SQLite database
  - RFC 5545 compliant iCal feed generation (VCALENDAR, VEVENT, VALARM)
  - Token-based subscription URLs (`webcal://host/cal/{token}.ics`)
  - Management API for feeds and events (POST/GET/DELETE)
  - Support for deadlines with automatic VALARM reminders
  - All-day events, categories, locations, URLs
  - Dockerfile for containerized deployment
  - Cloud Run deployment with GCS volume mount for SQLite persistence
  - Terraform infrastructure (Cloud Run Gen2 + GCS bucket)
  - GitHub Actions CI/CD workflow for automated deployment
- Initial project structure
- SMS webhook endpoint (responds "world" to any incoming message)
- CONTEXT.md for development state tracking
- CHANGELOG.md for release tracking
- AGPL-3.0 license
