# Security

This provider is under development and has no published supported release yet. Security fixes are maintained on the development branch; release support periods will be stated when releases exist. This community project does not promise a response or remediation deadline.

## Reporting

Use GitHub's private vulnerability reporting option in this repository's **Security** tab when available. Do not put credentials, exploitable details, Terraform state or sensitive response bodies in public issues. If private reporting is unavailable, use a private contact method published by the [maintainer](https://github.com/Edson-Edwin); a public issue may request a private reporting channel without describing the vulnerability.

Before public release, the maintainer must enable and verify private vulnerability reporting or publish a working private security contact. This is a release prerequisite, not a claim that the GitHub setting is already enabled.

## Credentials and state

OAuth credentials belong in environment variables or sensitive input variables. Sensitive resource fields, including webhook URLs, headers, parameters and bodies, remain in Terraform state even when Terraform hides them in normal output. Protect state storage and access. Avoid sharing debug logs unless they have been reviewed for secrets.

The client blocks redirects, limits decoded response sizes and exposes only controlled diagnostics. Unknown server error codes, response messages and raw transport errors are not printed. These safeguards do not replace secure state handling or review of future changes.
