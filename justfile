templ:
    templ generate

gen: templ

run files_root="" max_download_size="10G": gen
    FILES_ROOT="{{files_root}}" FILES_MAX_DOWNLOAD_SIZE="{{max_download_size}}" go run ./cmd/

dev files_root="" max_download_size="10G":
    FILES_ROOT="{{files_root}}" FILES_MAX_DOWNLOAD_SIZE="{{max_download_size}}" air
