package rate

import (
	"fmt"
	"reflect"
	"strconv"

	"github.com/Nigel2392/goldcrest"
)

func init() {
	RegisterMatchKind(func(v string) []string { return []string{v} })
	RegisterMatchKind(func(v []byte) []string { return []string{string(v)} })

	RegisterMatchKind(func(v int) []string { return []string{strconv.FormatInt(int64(v), 10)} })
	RegisterMatchKind(func(v int8) []string { return []string{strconv.FormatInt(int64(v), 10)} })
	RegisterMatchKind(func(v int16) []string { return []string{strconv.FormatInt(int64(v), 10)} })
	RegisterMatchKind(func(v int32) []string { return []string{strconv.FormatInt(int64(v), 10)} })
	RegisterMatchKind(func(v int64) []string { return []string{strconv.FormatInt(int64(v), 10)} })

	RegisterMatchKind(func(v uint) []string { return []string{strconv.FormatUint(uint64(v), 10)} })
	RegisterMatchKind(func(v uint8) []string { return []string{strconv.FormatUint(uint64(v), 10)} })
	RegisterMatchKind(func(v uint16) []string { return []string{strconv.FormatUint(uint64(v), 10)} })
	RegisterMatchKind(func(v uint32) []string { return []string{strconv.FormatUint(uint64(v), 10)} })
	RegisterMatchKind(func(v uint64) []string { return []string{strconv.FormatUint(uint64(v), 10)} })

}

const HOOK_GET_SESSION_ID_FOR_TYPE = "GET_SESSION_ID_FOR_TYPE"

type (
	MatchType            interface{}
	SessionIDForTypeHook = func(typ reflect.Type, data MatchType) ([]string, bool)
	UniqueStringFunc     = func(data MatchType) []string
	registry             struct {
		typData map[reflect.Type][]UniqueStringFunc
		kndData map[reflect.Kind][]UniqueStringFunc
	}
)

var r = &registry{
	typData: make(map[reflect.Type][]UniqueStringFunc),
	kndData: make(map[reflect.Kind][]UniqueStringFunc),
}

func RegisterMatchType[T MatchType](str func(data T) []string) {
	appendToMap(r.typData, reflect.TypeFor[T](), func(data MatchType) []string { return str(data.(T)) })
}

func RegisterMatchKind[T MatchType](str func(data T) []string) {
	var toT = reflect.TypeFor[T]()
	appendToMap(r.kndData, toT.Kind(), func(data MatchType) []string {
		var rV = reflect.ValueOf(data)
		if rV.IsValid() && rV.CanConvert(toT) {
			rV = rV.Convert(toT)
		}
		return str(rV.Interface().(T))
	})
}

func SessionIDForType[T MatchType](val T) []string {
	k := reflect.TypeFor[T]()
	fns, ok := r.typData[k]
	if ok {
		s := make([]string, 0, len(fns))
		for _, fn := range fns {
			s = append(s, fn(val)...)
		}
		return s
	}

	fns, ok = r.kndData[k.Kind()]
	if ok {
		s := make([]string, 0, len(fns))
		for _, fn := range fns {
			s = append(s, fn(val)...)
		}
		return s
	}

	var sessId = make([]string, 0)
	for _, hook := range goldcrest.Get[SessionIDForTypeHook](HOOK_GET_SESSION_ID_FOR_TYPE) {
		add, ok := hook(k, val)
		if ok {
			sessId = append(sessId, add...)
		}
	}

	if len(sessId) == 0 {
		panic(fmt.Sprintf("no session id could be generated for %T", val))
	}

	return sessId
}

//
//	type StringKey string
//
//	func (s StringKey) SessionID() []string { return []string{string(s)} }
//
//	type SliceKey []string
//
//	func (s SliceKey) SessionID() []string { return []string(s) }
//
