package util

func Map[I, R any](input []I, f func(I) R) []R {
	result := make([]R, len(input))
	for i := range input {
		result[i] = f(input[i])
	}
	return result
}

func FlatMap[I, R any](input []I, f func(I) []R) []R {
	var result []R
	for _, v := range input {
		result = append(result, f(v)...)
	}
	return result
}

func Filter[I any](input []I, f func(I) bool) []I {
	var result []I
	for _, v := range input {
		if f(v) {
			result = append(result, v)
		}
	}
	return result
}

func Filter2[I any](input []I, f func(I) bool) []I {
	return FlatMap(input, func(v I) []I {
		if f(v) {
			return []I{v}
		} else {
			return []I{}
		}
	})
}
