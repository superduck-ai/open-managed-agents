# Cloud adapter verification

These commands execute the production storage/E2B adapters against explicitly configured cloud services. They do not start the local backend or a scripted Worker. Unit tests using HTTP fixtures verify the verifier itself; only an actual cloud run can produce a cloud pass report.

Use a dedicated test bucket and E2B account/project. Save a private YAML file with mode 0600 or 0400, at most 64 KiB. `.verify-be.cloud.local.yaml` is ignored by Git. Credentials are never copied into the evidence directory or displayed by help/doctor.

```yaml
purpose: verify-be
storage:
  endpoint: https://YOUR-S3-ENDPOINT
  region: YOUR-REGION
  bucket: YOUR-TEST-BUCKET
  access_key_id: OWNER-KEY
  secret_access_key: OWNER-SECRET
  force_path_style: false
read_only:
  access_key_id: DISTINCT-READ-ONLY-KEY
  secret_access_key: READ-ONLY-SECRET
e2b:
  api_key: YOUR-E2B-TEST-KEY
  template: YOUR-TEST-TEMPLATE
  debug: false
```

Only the section needed by the chosen scenario is required. Optional E2B `api_url`, `sandbox_url`, `domain` and `access_token` support a dedicated deployment. Explicit endpoints require HTTPS without userinfo, query strings or fragments. The config is an external input, so do not commit real values. The verifier does not provision IAM policies, buckets, templates or shared network rules.

```sh
just verify-be files doctor cloud-storage --cloud-config /private/cloud.yaml
just verify-be files cloud-storage --cloud-config /private/cloud.yaml
just verify-be chat doctor cloud-renewal --cloud-config /private/cloud.yaml
just verify-be chat cloud-renewal --cloud-config /private/cloud.yaml
```

`VERIFY_BE_CLOUD_CONFIG` can provide the path instead. Missing/invalid local config exits 2 with blocked status and no cloud requests. Doctor validates config only; remote permission or network failures during execution are failures, not passes. No Docker, Worker image or local service configuration is required. The outer CLI still uses the repository checkout for source provenance.

## Storage assertions

The owner uploads one small binary object at `verify-be/<run-id>/content.bin`. Owner and read-only credentials must download identical bytes. The read-only principal must receive AccessDenied for PUT and DELETE, and the owner's original bytes must remain unchanged. This verifies one explicit IAM contract and actual network access; it does not certify every account policy, firewall rule or network failure mode.

Owner credentials need GetObject, PutObject, DeleteObject and GetBucketVersioning. Versioned buckets also need ListBucketVersions and DeleteObjectVersion so cleanup can remove every version and delete marker for the exact owned key. The read-only principal needs GetObject on the run prefix and must not have PutObject/DeleteObject there. Nothing outside the generated key is deleted; bucket policy/versioning is never changed.

## E2B assertions

The production Provider creates one sandbox tagged `verify_be_run=<run-id>` with a one-minute lifetime and no internet access. It calls the production `SetTimeout` adapter with three minutes, then reads the provider's deadline and requires an extension within a bounded tolerance. Finally it kills the sandbox and requires a provider not-found response. It does not submit a public chat tool confirmation or verify a cloud Worker callback into a deployed backend.

## Cleanup and CI

`cloud-resources.json` records the generated object key or sandbox ID, never credentials. Cleanup runs with a separate bounded context after errors/cancellation. A failed cleanup prevents pass. If sandbox creation has an unknown outcome, cleanup remains unconfirmed; inspect the provider for the run tag. After SIGKILL/host failure, use the manifest and original private config to remove only these resources and confirm absence. E2B's pause-on-timeout policy is not evidence of sandbox deletion.

The manual workflow input `verify_cloud=true` enables `Cloud adapter verification` using the GitHub environment `verification-cloud` and its `VERIFY_BE_CLOUD_CONFIG_YAML` secret. Configure environment protection and credentials separately. This job does not run on PRs and does not accept cloud secrets from PR code automatically. It uploads only verdicts and owned resource IDs. A local implementation or mock test pass is not a remote CI/cloud execution result.
