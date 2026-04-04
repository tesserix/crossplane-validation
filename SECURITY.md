# Security Policy

## Reporting a Vulnerability

If you discover a security vulnerability, please report it responsibly.

**Do NOT open a public GitHub issue for security vulnerabilities.**

Instead, email: samyak.rout@gmail.com

Include:
- Description of the vulnerability
- Steps to reproduce
- Potential impact
- Suggested fix (if any)

We will acknowledge your report within 48 hours and aim to release a fix within 7 days for critical issues.

## Supported Versions

| Version | Supported |
|---------|-----------|
| latest  | Yes       |

## Security Best Practices

When using `crossplane-validate`:

- Use read-only cloud credentials for the `--cloud` mode
- Never commit cloud credentials to the repository
- Use GitHub Actions secrets or environment variables for CI credentials
- Review the plan output before applying changes to your cluster
