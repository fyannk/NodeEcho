#!/usr/bin/env bash
# Checks every commit of a range, as the pull request check does:
#   - the subject follows Conventional Commits;
#   - the commit carries a Signed-off-by trailer with the author's email
#     (Developer Certificate of Origin); commits by bots are exempt.
# Usage: hack/check-commits.sh <base> <head>
set -euo pipefail

base=$1
head=$2
types='build|chore|ci|docs|feat|fix|perf|refactor|revert|test'
subject_re="^($types)(\([^)]+\))?!?: [^ ]"
failed=0

while read -r sha; do
	subject=$(git log -1 --format=%s "$sha")
	author=$(git log -1 --format=%ae "$sha")
	trailers=$(git log -1 --format=%B "$sha" | git interpret-trailers --parse)
	if ! [[ $subject =~ $subject_re ]]; then
		echo "::error::${sha:0:12}: not a Conventional Commits subject: $subject"
		failed=1
	fi
	if [[ $author != *"[bot]"* ]] && ! grep -qiF "Signed-off-by: " <<<"$trailers"; then
		echo "::error::${sha:0:12}: no Signed-off-by trailer (git commit -s): $subject"
		failed=1
	elif [[ $author != *"[bot]"* ]] && ! grep -qiF "<$author>" <<<"$(grep -i '^Signed-off-by: ' <<<"$trailers")"; then
		echo "::error::${sha:0:12}: no Signed-off-by with the author's email <$author>: $subject"
		failed=1
	fi
done < <(git rev-list --no-merges "$base..$head")

if [[ $failed -ne 0 ]]; then
	echo "see CONTRIBUTING.md" >&2
	exit 1
fi
echo "commits ok: $(git rev-list --no-merges --count "$base..$head") checked"
