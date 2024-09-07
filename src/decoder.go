package main

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

type BencodingDataType int

const (
	Int BencodingDataType = iota
	String
	List
	Dictionary

	Unknown
)

type Decoder struct {
	value    string
	curIndex int
}

func (d *Decoder) parseString() (string, error) {
	var bencodedString = d.value[d.curIndex:]

	var firstColonIndex int = -1
	for i := 0; i < len(bencodedString); i++ {
		if bencodedString[i] == ':' {
			firstColonIndex = i
			break
		}
	}
	if firstColonIndex == -1 {
		return "", fmt.Errorf("can't parse string, expected format: <string length encoded in base ten ASCII>:<string data>. Received: %s", bencodedString)
	}

	lengthStr := bencodedString[:firstColonIndex]

	length, err := strconv.Atoi(lengthStr)
	if err != nil {
		return "", err
	}

	decodedString := strings.Clone(bencodedString[firstColonIndex+1 : firstColonIndex+length+1])
	d.curIndex += firstColonIndex + length + 1
	return decodedString, nil
}

func (d *Decoder) parseInt() (int, error) {
	var bencodedInt = d.value[d.curIndex:]

	var terminationIndex int = -1
	for i := 0; i < len(bencodedInt); i++ {
		if bencodedInt[i] == 'e' {
			terminationIndex = i
			break
		}
	}
	if terminationIndex == -1 {
		return 0, fmt.Errorf("can't parse int, expected format: i<integer encoded in base ten ASCII>e. Received: %s", bencodedInt)
	}

	intValue, err := strconv.Atoi(bencodedInt[:terminationIndex])
	if err != nil {
		return -1, err
	}

	d.curIndex += terminationIndex + 1
	return intValue, nil
}

func (d *Decoder) decode() interface{} {
	var decodedValue interface{}
	switch d.determineDataType() {
	case Int:
		result, err := d.parseInt()
		if err != nil {
			abort(err.Error())
		}

		decodedValue = result
	case String:
		result, err := d.parseString()
		if err != nil {
			abort(err.Error())
		}

		decodedValue = result
	case List:
		result := make([]interface{}, 0)
		for i := d.curIndex; i < len(d.value); i = d.curIndex {
			if d.value[i] == 'e' {
				break
			}

			data := d.decode()
			result = append(result, data)
		}

		d.curIndex++
		decodedValue = result
	case Dictionary:
		result := map[string]interface{}{}

		for i := d.curIndex; i < len(d.value); i = d.curIndex {
			if d.value[i] == 'e' {
				break
			}

			key := d.decode()
			value := d.decode()

			keyStr, ok := key.(string)
			if !ok {
				abort(fmt.Sprintf("Dictionary key must be string. Received: %s", keyStr))
			}

			result[keyStr] = value
		}

		d.curIndex++
		decodedValue = result
	case Unknown:
		abort(fmt.Sprintf("Unsupported encoding received. CurIndex: %d | Value: %s", d.curIndex, d.value))
	}

	return decodedValue
}

func (d *Decoder) determineDataType() BencodingDataType {
	value := d.value[d.curIndex]
	d.curIndex++
	if unicode.IsDigit(rune(value)) {
		// bencoded strings look like this => 5:hello
		// The first value '5' is needed to know how long is the string,
		// thus string is the only datatype for which we must not skip the first value :)
		d.curIndex--
		return String
	} else if value == 'i' {
		return Int
	} else if value == 'l' {
		return List
	} else if value == 'd' {
		return Dictionary
	} else {
		return Unknown
	}
}
