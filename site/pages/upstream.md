# Relationship to upstream

[Vikunja](https://github.com/go-vikunja/vikunja) is an open-source, self-hosted to-do and project app written by the Vikunja maintainers and contributors. vikunja-enterprise is a fork of it. Almost everything you see in the application is their work, and this site would have nothing to describe without it.

> [!IMPORTANT]
> vikunja-enterprise is an independent community fork. It is not affiliated with, endorsed by or supported by the Vikunja project or its maintainers. Please do not ask them for help with this fork, and do not send them its bug reports. If Vikunja is useful to you, [support the upstream project](https://vikunja.io/support/).

## What the fork is

The fork is built and maintained by [Bahadır Doğru](https://bahadirdogru.com), who is not part of the Vikunja project. Other work by the same author is listed on [bahadirdogru.com](https://bahadirdogru.com).

- The same Go API, Vue 3 frontend, database layer and single-binary deployment, plus six modules that live in their own packages, files and tables: [custom fields](../../docs/modules/custom-fields.md), [workflows](../../docs/modules/workflows.md), [approvals](../../docs/modules/approvals.md), [automation](../../docs/modules/automation.md), [dashboards and reports](../../docs/modules/dashboards-reports.md) and [forms](../../docs/modules/forms.md).
- A development project. The aim in the name is to offer a copy of Vikunja that is complete and properly built in every respect up to enterprise level. It is an aim, not a claim; the [roadmap](../../docs/enterprise/ROADMAP.md) says what is done, what was tested on which database, and what is open.
- Licensed under AGPL-3.0-or-later, as upstream, with the same terms for anyone who runs it as a service.

## What the fork is not

- Not an official or supported distribution of Vikunja, and not a commercial offering.
- Not production-ready by virtue of its name. "Enterprise" describes the direction of the work.
- Not a replacement for upstream's own documentation. Everything the fork does not change, upstream documents.
- Not a drop-in for upstream's releases. Its database has extra tables, and moving an instance between the two in either direction has not been tested.

{{include README.md | Relationship to upstream | body}}

## Where upstream's documentation applies

For these topics the fork changes nothing, so upstream's pages are the reference. They are upstream's and describe upstream's releases:

- [Installing](https://vikunja.io/docs/installing/) and [configuration options](https://vikunja.io/docs/config-options/)
- [User guide](https://vikunja.io/docs/) for projects, tasks, kanban, sharing, filters
- Mail, authentication providers (OpenID Connect, LDAP), CalDAV, migration from other tools and the [webhooks](https://vikunja.io/docs/webhooks/) of upstream

Where the fork adds to one of these, for example the automation module's own incoming webhooks, the fork's [module pages](../../docs/modules/README.md) say so.

## When to use upstream instead

Use Vikunja itself, not this fork, if:

- you only need what Vikunja already does, since upstream is the better tested and more widely used choice for that;
- you need support from the people who build it, official releases and packages, or the hosted service;
- you want to run something you can rely on in production today, and you do not want to read a roadmap first;
- you use the official desktop or mobile apps and want the combination upstream tests.

Use the fork if you need custom fields, status workflows, approvals, rule-based automation, dashboards or forms, and you accept that it is a development fork you run at your own risk.

## Changes that belong upstream

If a bug or an improvement is not specific to the fork's modules, it is better reported to or contributed to [Vikunja](https://github.com/go-vikunja/vikunja) so that everyone benefits. The fork keeps its own difference small so that such fixes can be merged in both directions.
