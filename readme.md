# FileSRV

Quick minimal readonly file server with some qol features.

- Fuzzy file search with the keybind `/`
- File and dir sizes.
- Download button for both files and dirs. (Dirs automatically zipped)

This was made quickly in an evening with generative AI. It cost $2.40.

## Env

`FILES_ROOT` : Directory to serve. Defaults to the current working directory.

`FILES_MAX_DOWNLOAD_SIZE` : Maximum downloadable file or directory size. Accepts
bytes or a case-insensitive binary suffix (`K`, `M`, `G`, or `T`), such as `10M`
for 10 MiB. Defaults to unlimited; leave unset or set to `0`. Larger items
remain browsable but cannot be downloaded.

`FILES_TRUSTED_PROXY_CIDRS` : Comma-separated trusted reverse-proxy CIDRs, such
as `10.42.0.0/16`. When set, FileSRV accepts requests only from these networks
and trusts their TinyAuth user headers and `X-Forwarded-For` values. Unset
preserves direct access and the existing forwarded-header behavior.
