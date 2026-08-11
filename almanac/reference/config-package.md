---
title: "Config Package"
summary: "The config package centralizes application configuration loading from environment variables with sensible defaults."
topics: [configuration]
sources:
  - id: config-package
    type: file
    path: internal/config/
  - id: config-go
    type: file
    path: internal/config/config.go
  - id: config-test
    type: file
    path: internal/config/config_test.go
---

The config package provides the single entry point for loading application configuration from environment variables. All settings are loaded once at startup via the `Load()` function, which returns a `Config` struct containing all application settings. The package automatically discovers and loads `.env` files by walking up from the current working directory until finding a `go.mod` file[@config-package].

## Config Structure

The `Config` struct contains configuration for all major subsystems: database connection, Redis caching, S3-compatible file storage, Anthropic API upstream integration, batch processing, E2B sandbox environments, webhook delivery, and various SDK fixtures for testing. Each field has a corresponding environment variable and a fallback default value suitable for local development [@config-go].

## Loading Process

Configuration loading follows a specific sequence: first, the package searches for a `.env` file starting from the current directory and walking upward until either a `.env` file is found or a `go.mod` file is encountered. Any `.env` file found is loaded via `godotenv`. Then environment variables are read using typed helper functions (`env`, `envBool`, `envInt64`, `envInt`, `envDuration`, `envCSV`, `firstNonEmpty`) which parse values and apply defaults. Finally, validation ensures required fields like `DATABASE_URL`, `S3_ENDPOINT`, `S3_BUCKET`, and S3 credentials are present [@config-go].

## Environment-Specific Defaults

Several configuration defaults vary by environment. In production or `prod` environments, `DatabaseAutoMigrate` defaults to false to prevent accidental schema changes, while in development it defaults to true. Similarly, `CodeSessionOTLPFileLogEnabled` defaults to true in development for debugging but false in production. These behaviors can be explicitly overridden via environment variables regardless of the detected environment [@config-go][@config-test].

## Validation

After loading all values, the `Load()` function validates that required fields are present. Specifically, it checks that `DATABASE_URL`, `S3_ENDPOINT`, `S3_BUCKET`, `S3_ACCESS_KEY_ID`, and `S3_SECRET_ACCESS_KEY` are non-empty strings. If any required field is missing, `Load()` returns an error describing which field is required [@config-go].

## Helper Functions

The package provides several helper functions for parsing environment variables with type safety and defaults: `envBool` recognizes common true/false representations (`1`, `true`, `t`, `yes`, `y`, `on` and their opposites), `envInt64` and `envInt` parse integers rejecting non-positive values, `envDuration` parses Go duration strings, and `envCSV` splits comma-separated values into string slices. The `firstNonEmpty` helper returns the first non-empty value from a list of candidates, used for URL configuration where fallback chains are common [@config-go].
