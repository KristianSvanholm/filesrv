templ:
    templ generate

gen: templ

run files_root="": gen
    FILES_ROOT="{{files_root}}" go run ./cmd/

dev files_root="":
    FILES_ROOT="{{files_root}}" air
