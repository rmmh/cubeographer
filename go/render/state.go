package render

import (
	"fmt"
	"math/bits"
	"sort"
	"strings"

	"github.com/rmmh/cubeographer/go/resourcepack"
)

func stringSliceSearch(slice []string, needle string) int {
	for i, v := range slice {
		if v == needle {
			return i
		}
	}
	return -1
}

func buildStateList(st *resourcepack.BlockState) [][]string {
	attrs := map[string][]string{}

	if st.Variants != nil {
		for pred := range st.Variants {
			if pred != "" && pred != "normal" {
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
			for _, conj := range part.When.Clauses {
				for attr, value := range conj {
					var val string
					switch v := value.(type) {
					case string:
						val = v
					case bool:
						if v {
							val = "true"
						} else {
							val = "false"
						}
					default:
						panic("unhandled when")
					}
					if val == "side|up" {
						val = "side" // literally used once
					}
					if stringSliceSearch(attrs[attr], val) == -1 {
						attrs[attr] = append(attrs[attr], val)
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
				fmt.Printf("UNARY STATEMAP ??? %#v %#v\n", values, attrs)
			}
			alist = append(alist, append([]string{name}, values...))
			bitsNeeded += bits.Len(uint(len(values) - 1))
		}
		if bitsNeeded > 16 {
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

type Statemaskval uint32
type Stateval uint16
type Statemap map[string]Statemaskval

// given a statelist input, like:
// [[half bottom top] [open false true] [facing east north south west]]
// return a map from a type (like half=bottom) to a uint16,
// where the low byte is the value and the high byte is the mask
func BuildStateMap(sl [][]string) Statemap {
	if len(sl) == 0 {
		return nil
	}
	m := map[string]Statemaskval{}
	offset := 0
	for _, attrs := range sl {
		attr := attrs[0]
		attrs = attrs[1:]
		attrBits := bits.Len(uint(len(attrs) - 1))
		if offset+attrBits > 16 {
			panic(fmt.Sprintf("too many attr bits for statemap %d>8 %#v", offset+attrBits, sl))
		}
		attrMask := Statemaskval((1<<attrBits)-1) << offset
		for n, name := range attrs {
			m[attr+"="+name] = attrMask<<16 | Statemaskval(n)<<offset
		}
		offset += attrBits
	}
	return m
}

func (s Statemap) Get(properties string) Stateval {
	return s.GetList(strings.Split(properties, ","))
}

func (s Statemap) GetList(props []string) Stateval {
	var state Stateval
	for _, pred := range props {
		state |= Stateval(s[pred])
	}
	return state
}

func (s Statemap) Max() Stateval {
	var state Stateval
	for _, v := range s {
		state |= Stateval(v)
	}
	return state
}
