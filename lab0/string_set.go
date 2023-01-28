package string_set

import (
	"sync"
	"fmt"
	"regexp"
)

type StringSet interface {
	// Add string s to the StringSet and return whether the string was inserted
	// in the set.
	Add(key string) bool

	// Return the number of unique strings in the set
	Count() int

	// Return all strings matching a regex `pattern` within a range `[begin,
	// end)` lexicographically (for Part C)
	PredRange(begin string, end string, pattern string) []string
}

type LockedStringSet struct {
	set map[string]bool
	lock sync.RWMutex
}

func MakeLockedStringSet() LockedStringSet {
	return LockedStringSet{set: make(map[string]bool)}
}

func (stringSet *LockedStringSet) Add(key string) bool {
	stringSet.lock.Lock()
	defer stringSet.lock.Unlock()
	_, ok := stringSet.set[key]
	// fmt.Println(rand.Intn(100))
	if ok == true {
		return false
	} else {
		stringSet.set[key] = true
		return true
	}
}

func (stringSet *LockedStringSet) Count() int {
	fmt.Print()
	stringSet.lock.RLock()
	defer stringSet.lock.RUnlock()
	
	return len(stringSet.set)
}

func (stringSet *LockedStringSet) PredRange(begin string, end string, pattern string) []string {
	// Result list
	results := make([]string, 0)

	stringSet.lock.RLock()
	defer stringSet.lock.RUnlock()

	for key, _ := range stringSet.set {
		re := regexp.MustCompile(pattern)
		if re.Match([]byte(key)) && begin <= key && key < end {
			// Found a result in range
			results = append(results, key)
		}
	}

	return results
}
