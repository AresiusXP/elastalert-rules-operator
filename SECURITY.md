# Security Policy

## Supported versions

Only the latest minor release receives security fixes.

## Reporting a vulnerability

Please do not open a public issue. Report vulnerabilities privately through
[GitHub Security Advisories](https://github.com/AresiusXP/elastalert-rules-operator/security/advisories/new).

Response targets:

- Acknowledgement within 3 business days.
- Initial assessment within 7 days.
- Fix or mitigation for confirmed high and critical issues within 30 days.

## Verifying releases

Release images and Helm charts are signed with keyless
[cosign](https://github.com/sigstore/cosign) from the release workflow, and the
image carries a build provenance attestation.

Verify the image signature:

```sh
cosign verify ghcr.io/aresiusxp/elastalert-rules-operator:<version> \
  --certificate-identity-regexp 'https://github.com/AresiusXP/elastalert-rules-operator/.github/workflows/release.yml@refs/tags/v.*' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

Verify the Helm chart signature:

```sh
cosign verify ghcr.io/aresiusxp/charts/elastalert-rules-operator:<version> \
  --certificate-identity-regexp 'https://github.com/AresiusXP/elastalert-rules-operator/.github/workflows/release.yml@refs/tags/v.*' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

Verify the build provenance attestation:

```sh
cosign verify-attestation ghcr.io/aresiusxp/elastalert-rules-operator:<version> \
  --type slsaprovenance1 \
  --certificate-identity-regexp 'https://github.com/AresiusXP/elastalert-rules-operator/.github/workflows/release.yml@refs/tags/v.*' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

The provenance attestation can also be checked with the GitHub CLI:

```sh
gh attestation verify oci://ghcr.io/aresiusxp/elastalert-rules-operator:<version> \
  --repo AresiusXP/elastalert-rules-operator
```
