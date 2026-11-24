# Security

{{include README.md | Security reports | body}}

## What can be reached without a login

Two parts of the fork are reachable without an account. Both are described in detail on their module pages; this is the short list of what the repository does about them. It is not a security audit.

- **Public forms** are served from `/forms/<token>` on the same address as the app. They have rate limits, a honeypot, size limits, an optional submission cap and a close date, and public attachments are off by default. A submission creates the task as the form owner through the normal create path. See [forms](../../docs/modules/forms.md).
- **Incoming webhooks** of automation rules use a secret address. A webhook with a signing secret is verified and protected against replays within a time window; one without is not signed and cannot be protected that way. See [automation](../../docs/modules/automation.md) and the [API reference](../../API.md).

Automation scripts run in a sandboxed interpreter with limits, outgoing HTTP requests are signed and guarded against requests to internal addresses, and project secrets are stored encrypted. The trade-offs are written down in the [architecture decisions](../../ARCHITECTURE.md#architecture-decisions).

## Reporting

For problems in the fork's own code, use the [security advisories of this repository](https://github.com/bahadirdogru/vikunja-enterprise/security/advisories) if private reporting is available there. Otherwise open an [issue](https://github.com/bahadirdogru/vikunja-enterprise/issues) that says you have a security report, without exploit details.

Problems that also exist in upstream Vikunja should go to upstream, under [upstream's policy](https://vikunja.io/security).

## Known gaps

The gaps that are known today are listed in the [roadmap](../../docs/enterprise/ROADMAP.md), which is updated more often than this page.
