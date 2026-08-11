#!/usr/bin/env bash

# Validates docs/compatibility.json for well-formedness, correct structure,
# newest-first ordering, and 1:1 correspondence with release tags.

set -o errexit
set -o pipefail
set -o nounset

if [[ -d ${GITHUB_WORKSPACE:-} ]]
then
    repo_root="$GITHUB_WORKSPACE"
else
    repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
fi
readonly repo_root

declare -r compat_file="$repo_root/docs/compatibility.json"
declare -i errors=0

if [[ ! -f $compat_file ]]
then
    echo "Error: $compat_file not found" >&2
    exit 1
fi

if ! jq . "$compat_file" > /dev/null 2>&1
then
    echo "Error: $compat_file is not valid JSON" >&2
    exit 1
fi

echo "Validating structure..."

declare -r interval_pattern='^\[(([0-9]+\.)+[0-9]+),(([0-9]+\.)+[0-9]+)\)$'

while IFS= read -r version
do
    erlang=$(jq -r --arg v "$version" '.[$v].erlang // empty' "$compat_file")
    elixir=$(jq -r --arg v "$version" '.[$v].elixir // empty' "$compat_file")

    if [[ -z $erlang ]]
    then
        echo "Error: $version is missing 'erlang' key" >&2
        (( ++errors ))
    elif [[ ! $erlang =~ $interval_pattern ]]
    then
        echo "Error: $version has invalid erlang range: $erlang" >&2
        (( ++errors ))
    fi

    if [[ -z $elixir ]]
    then
        echo "Error: $version is missing 'elixir' key" >&2
        (( ++errors ))
    elif [[ ! $elixir =~ $interval_pattern ]]
    then
        echo "Error: $version has invalid elixir range: $elixir" >&2
        (( ++errors ))
    fi
done < <(jq -r 'keys_unsorted[]' "$compat_file")

echo "Validating newest-first ordering..."

declare -a json_versions
while IFS= read -r version
do
    json_versions+=("$version")
done < <(jq -r 'keys_unsorted[]' "$compat_file")

declare -a sorted_versions
while IFS= read -r version
do
    sorted_versions+=("$version")
done < <(printf '%s\n' "${json_versions[@]}" | sort -V -r)

for (( i=0; i<${#json_versions[@]}; i++ ))
do
    if [[ ${json_versions[$i]} != "${sorted_versions[$i]}" ]]
    then
        echo "Error: ordering mismatch at position $i: found ${json_versions[$i]}, expected ${sorted_versions[$i]}" >&2
        (( ++errors ))
        break
    fi
done

echo "Validating against release tags..."

declare -a expected_versions
while IFS= read -r tag
do
    expected_versions+=("${tag#v}")
done < <(git -C "$repo_root" tag -l 'v3.11.*' 'v3.12.*' 'v3.13.*' 'v4.*' \
    | grep -v -E '(beta|rc|alpha)' \
    | sort -V -r)

for version in "${expected_versions[@]}"
do
    if ! jq -e --arg v "$version" 'has($v)' "$compat_file" > /dev/null 2>&1
    then
        echo "Error: release tag v$version has no entry in compatibility.json" >&2
        (( ++errors ))
    fi
done

for version in "${json_versions[@]}"
do
    if ! git -C "$repo_root" rev-parse "v$version" > /dev/null 2>&1
    then
        echo "Error: compatibility.json has entry $version with no matching release tag" >&2
        (( ++errors ))
    fi
done

if (( errors > 0 ))
then
    echo "Validation failed with $errors error(s)" >&2
    exit 1
fi

echo "Validation passed (${#json_versions[@]} entries)"
