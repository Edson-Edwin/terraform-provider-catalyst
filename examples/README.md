# Examples

This directory contains examples that are mostly used for documentation, but can also be run/tested manually via the Terraform CLI.

For one configuration containing every currently registered resource and data
source, see [all_services/main.tf](all_services/main.tf) and its
[setup instructions](all_services/README.md). Optional inputs control which
resources are deployed; experimental and deprecated features require separate
opt-ins. This is a catalog of implemented subsets, not complete service coverage.

The document generation tool looks for files in the following locations by default. All other *.tf files besides the ones mentioned below are ignored by the documentation tool. This is useful for creating examples that can run and/or are testable even if some parts are not relevant for the documentation.

* **provider/provider.tf** example file for the provider index page
* **data-sources/`full data source name`/data-source.tf** example file for the named data source page
* **resources/`full resource name`/resource.tf** example file for the named data source page

New inventory examples cover `catalyst_datastore_tables`, `catalyst_filestore_folders`, `catalyst_cache_segments` and `catalyst_stratus_buckets`. The `catalyst_filestore_file` example includes harmless `settings.json` content and requires an existing folder. It demonstrates `filesha256` so changed local content plans replacement. File Store is legacy storage; consider Catalyst's Stratus guidance for new applications.
