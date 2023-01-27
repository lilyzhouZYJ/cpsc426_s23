package string_set

import (
	// "fmt"
	"sync"
	"sync/atomic"
)

// The StripedStringSet is an array of stripeCount stripes
// where each stripe is of type StringSetStripe.
type StripedStringSet struct {
	stripes []StringSetStripe
	stripeCount int

	count int32 // atomic counter
}

// Define structure for each stripe
type StringSetStripe struct {
	set map[string]bool		// set for this stripe
	lock sync.RWMutex		// lock for this stripe
}

// Hash function to determine striping:
// from https://github.com/orcaman/concurrent-map/blob/master/concurrent_map.go
func fnv32(key string) uint32 {
	hash := uint32(2166136261)
	const prime32 = uint32(16777619)
	keyLength := len(key)
	for i := 0; i < keyLength; i++ {
		hash *= prime32
		hash ^= uint32(key[i])
	}
	return hash
}

// Constructor
func MakeStripedStringSet(stripeCount int) StripedStringSet {
	// return StripedStringSet{make([]StringSetStripe, stripeCount, stripeCount), stripeCount}
	return StripedStringSet{make([]StringSetStripe, stripeCount, stripeCount), stripeCount, 0}
}

func (stringSet *StripedStringSet) Add(key string) bool {
	// (a) Figure out which stripe to put the key in
	index := fnv32(key) % uint32(stringSet.stripeCount)

	// (b) Find the stripe
	targetStripe := &stringSet.stripes[index]

	// (c) Add to the stripe
	targetStripe.lock.Lock()
	defer targetStripe.lock.Unlock()

	// c.1 make sure the set is initialized
	if targetStripe.set == nil {
		// Check for nil (uninitialized) stripe sets
		targetStripe.set = make(map[string]bool)
	}
	
	// c.2 add string to set
	_, ok := targetStripe.set[key]

	if ok == true {
		return false
	} else {
		targetStripe.set[key] = true

		atomic.AddInt32(&(stringSet.count), 1) // atomic counter
		return true
	}
}

func (stringSet *StripedStringSet) Count() int {
	// // Total count
	// sum := 0

	// // Iterate through every stripe, and for each:
	// // (a) Read lock the stripe
	// // (b) Add size of stripe to sum
	// for i := 0; i < stringSet.stripeCount; i++ {
	// 	stringSet.stripes[i].lock.RLock()
	// 	defer stringSet.stripes[i].lock.RUnlock()

	// 	sum += len(stringSet.stripes[i].set)
	// }

	// return sum

	result := atomic.LoadInt32(&stringSet.count)
	return int(result)
}

func (stringSet *StripedStringSet) PredRange(begin string, end string, pattern string) []string {
	return make([]string, 0)
}
