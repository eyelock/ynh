# Security Policy

## Reporting a vulnerability

Please report security issues privately. Do not open a public issue or discussion.

Use GitHub private vulnerability reporting: go to the
[Security tab](https://github.com/eyelock/ynh/security) of this repository and choose
"Report a vulnerability". If you cannot use that, email
[support@eyelock.net](mailto:support@eyelock.net). Include the version (`ynh version`), what you did, what you
expected and what happened, and a proof of concept if you have one.

You can expect an acknowledgement within a few days. Fixes are developed in a private
advisory, released, and then disclosed with credit to the reporter unless you prefer
otherwise.

## Supported versions

Security fixes go into the latest release. Older releases are not patched.

## Scope

ynh assembles vendor-specific layouts from harnesses and launches the vendor CLI. In scope:
path handling and deletion guards, fetching and installing harnesses from remote sources,
the sensors and hooks a harness can run, and the release artifacts and container image.
Vulnerabilities in a vendor CLI, or in a harness someone else authored, belong with their
maintainers.
