{{/* Keep the admission-policy grammar aligned with metric/simple_parser.go. */}}
{{- define "otel-add-on.metricQueryRegexp" -}}
{{- $doubleQuoted := "\"(?:[^\"\\\\\\n]|\\\\.)*\"" -}}
{{- $singleQuoted := "'(?:[^'\\\\\\n]|\\\\.)*'" -}}
{{- $rawQuoted := "`[^`]*`" -}}
{{- $doubleQuotedName := "\"(?:[^\"\\\\\\n]|\\\\.)+\"" -}}
{{- $singleQuotedName := "'(?:[^'\\\\\\n]|\\\\.)+'" -}}
{{- $rawQuotedName := "`[^`]+`" -}}
{{- $bareName := "[^\\s=!~<>,{}()\"'`]+" -}}
{{- $bareValue := "[^\\s=!~{}(),\"'`]+" -}}
{{- $metricName := "[^\\s{}(),=!~\"'\\[\\]`]+" -}}
{{- $quotedValue := printf "(?:%s|%s|%s)" $doubleQuoted $singleQuoted $rawQuoted -}}
{{- $labelName := printf "(?:%s|%s|%s|%s)" $bareName $doubleQuotedName $singleQuotedName $rawQuotedName -}}
{{- $matcher := printf "%s\\s*(?:!=|!~|=~|=)\\s*(?:%s|%s)" $labelName $quotedValue $bareValue -}}
{{- $labels := printf "\\{\\s*%s(?:\\s*,\\s*%s)*\\s*,?\\s*\\}" $matcher $matcher -}}
{{- $selector := printf "%s(?:\\s*%s)?" $metricName $labels -}}
{{- printf "^\\s*(?:%s|(?:avg|count|min|max|sum)\\(\\s*%s\\s*\\))\\s*$" $selector $selector -}}
{{- end -}}
