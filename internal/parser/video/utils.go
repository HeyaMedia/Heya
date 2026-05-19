package video

func removeEmpty(m map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})
	for k, v := range m {
		if v != nil {
			result[k] = v
		}
	}
	return result
}
