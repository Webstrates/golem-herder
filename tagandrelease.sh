#!/usr/bin/env bash
git tag -a "$1" -m "$2"
gox --output "out/{{.Dir}}_$1_{{.OS}}_{{.Arch}}"
ghr -u Webstrates -r golem-herder "$1" out
