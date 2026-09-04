# FileSRV

Quick minimal readonly file server with some qol features.

- Fuzzy file search with the keybind `/`
- File and dir sizes.
- Download button for both files and dirs. (Dirs automatically zipped)

Set `FILES_ROOT` to the directory to serve; it defaults to the current working directory.

Set `FILES_MAX_DOWNLOAD_SIZE` to a maximum download size. Use bytes or a case-insensitive binary suffix: `K`, `M`, `G`, or `T` (for example, `10M` is 10 MiB). Files and directories larger than this limit cannot be downloaded; unset or set it to `0` for no limit.

This was made quickly in an evening with generative AI. It cost $2.40.
