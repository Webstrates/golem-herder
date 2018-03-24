#!/usr/bin/env bash
git co master && git merge develop && git tag -a "$1" -m "$2" && git push && git push --tags && git co develop || true
rm -fr out && mkdir out
gox --output "out/{{.Dir}}_$1_{{.OS}}_{{.Arch}}"
ghr -u Webstrates -r golem-herder "$1" out
