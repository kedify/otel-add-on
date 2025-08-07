#!/bin/bash
[[ $# -lt 2 ]] && echo "usage: ${0} <base> <overlay>]" && exit 1

export overlay="${2}"
export idPath=".name"
export originalPath=".otelOperatorCrs"
export otherPath=".otelOperatorCrs"

yq eval-all '
(
  (( (eval(strenv(originalPath)) + eval(strenv(otherPath)))  | .[] | {(eval(strenv(idPath))):  .}) as $item ireduce ({}; . * $item )) as $uniqueMap
  | ( $uniqueMap  | to_entries | .[]) as $item ireduce([]; . + $item.value)
) as $mergedArray
| . *= load(strenv(overlay)) | select(fi == 0) | (eval(strenv(originalPath))) = $mergedArray
' "${1}" "${2}"
