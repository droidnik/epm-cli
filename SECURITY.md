# Security policy

epm-cli handles credentials for BeyondTrust EPM tenants (OAuth client ID and secret, access tokens) and can
change or delete data, so security reports are taken seriously.

## Reporting a vulnerability

Please **do not open a public issue**. Report privately through GitHub:
**Security › Report a vulnerability** in this repository
(<https://github.com/droidnik/epm-cli/security/advisories/new>).

Include the version (`epm-cli version`), the platform and the steps to reproduce. You will get a first
answer within a few days. Fixes are released as a new version; the advisory credits the reporter unless
you prefer otherwise.

## Supported versions

Only the latest release receives fixes.

## How credentials are handled

- The client secret is stored in the macOS Keychain (service `epm-cli`). On other systems it is stored in
  `~/.config/epm-cli/secrets.json` with mode 0600. In CI, use environment variables instead.
- Access tokens are cached in `~/.cache/epm-cli/tokens/` with mode 0600 and are valid for about an hour.
- Secrets and tokens are never written to logs, also not with `--verbose`.
- Never share the output of `epm-cli auth token`, it is a valid bearer token.
