package main

import (
	"fmt"
	"slices"
	"strings"
)

// TODO: add support for encoding lists and dictionaries
func encodeDict(dictData map[string]interface{}) string {
	var mapKeys []string
	for key := range dictData {
		mapKeys = append(mapKeys, key)
	}
	// Specification says that keys must appear in sorted order
	slices.Sort(mapKeys)

	var result strings.Builder
	result.WriteString("d")
	for i := 0; i < len(mapKeys); i++ {
		key := mapKeys[i]
		value := dictData[key]

		bencodedKey := fmt.Sprintf("%d:%s", len(key), key)
		result.WriteString(bencodedKey)
		switch typedValue := value.(type) {
		case int:
			bencodedInt := fmt.Sprintf("i%de", typedValue)
			result.WriteString(bencodedInt)
		case string:
			bencodedString := fmt.Sprintf("%d:%s", len(typedValue), typedValue)
			result.WriteString(bencodedString)
		default:
			abort(fmt.Sprintf("Unsupported data type received for encoding. Received: %x", value))
		}
	}
	result.WriteString("e")

	return result.String()
}
