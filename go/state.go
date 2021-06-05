package main

import (
	"fmt"
	"math/bits"
	"sort"
	"strings"
)

func stringSliceSearch(slice []string, needle string) int {
	for i, v := range slice {
		if v == needle {
			return i
		}
	}
	return -1
}

func (st *blockState) buildStateList() [][]string {
	attrs := map[string][]string{}

	if st.Variants != nil {
		for pred := range st.Variants {
			if pred != "" {
				parts := strings.Split(pred, ",")
				for _, part := range parts {
					equiv := strings.SplitN(part, "=", 2)
					if stringSliceSearch(attrs[equiv[0]], equiv[1]) == -1 {
						attrs[equiv[0]] = append(attrs[equiv[0]], equiv[1])
					}
				}
			}
		}
	}

	for _, part := range st.Multipart {
		if part.When != nil {
			for _, conj := range part.When {
				for attr, value := range conj {
					if value == "side|up" {
						value = "side" // literally used once
					}
					if stringSliceSearch(attrs[attr], value) == -1 {
						attrs[attr] = append(attrs[attr], value)
					}
				}
			}
		}
	}

	if len(attrs) > 0 {
		bitsNeeded := 0
		alist := [][]string{}
		for name, values := range attrs {
			if len(values) == 1 && values[0] == "true" {
				attrs[name] = append(attrs[name], "false")
				values = attrs[name]
			}
			sort.Strings(values)
			if len(values) == 1 {
				fmt.Println("UNARY STATEMAP ???", values)
			}
			alist = append(alist, append([]string{name}, values...))
			bitsNeeded += bits.Len(uint(len(values) - 1))
		}
		if bitsNeeded > 8 {
			fmt.Printf("STATEMAP TOO BIG: %d %#v\n%#v\n", bitsNeeded, attrs, st)
		}
		sort.Slice(alist, func(i, j int) bool {
			if len(alist[i]) != len(alist[j]) {
				return len(alist[i]) < len(alist[j])
			}
			return alist[i][0] < alist[j][0]
		})
		if false {
			fmt.Println("STATEMAP", alist)
		}
		return alist
	}

	return nil
}

type statemap map[string]uint16

// given a statelist input, like:
// [[half bottom top] [open false true] [facing east north south west]]
// return a map from a type (like half=bottom) to a uint16,
// where the low byte is the value and the high byte is the mask
func buildStateMap(sl [][]string) statemap {
	if len(sl) == 0 {
		return nil
	}
	m := map[string]uint16{}
	offset := 0
	for _, attrs := range sl {
		attr := attrs[0]
		attrs = attrs[1:]
		attrBits := bits.Len(uint(len(attrs) - 1))
		if offset+attrBits > 8 {
			panic(fmt.Sprintf("too many attr bits for statemap %d>8 %#v", offset+attrBits, sl))
		}
		attrMask := ((1 << attrBits) - 1) << offset
		for n, name := range attrs {
			m[attr+"="+name] = uint16(attrMask<<8) | uint16(n<<offset)
		}
		offset += attrBits
	}
	return m
}

func (s statemap) getState(properties string) uint8 {
	return s.getStateList(strings.Split(properties, ","))
}

func (s statemap) getStateList(props []string) uint8 {
	var state uint8
	for _, pred := range props {
		state |= uint8(s[pred])
	}
	return state
}

func (s statemap) max() uint8 {
	var state uint8
	for _, v := range s {
		state |= uint8(v)
	}
	return state
}
