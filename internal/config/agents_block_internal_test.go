package config

import (
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestAgentsBlockFieldsMatchAgentsConfig(t *testing.T) {
	t.Parallel()
	var tags []string
	for _, field := range reflect.VisibleFields(reflect.TypeFor[AgentsConfig]()) {
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		tags = append(tags, name)
	}
	slices.Sort(tags)
	if !slices.Equal(tags, agentsBlockFields) {
		t.Fatalf("AgentsConfig json fields = %v, agentsBlockFields = %v; a field must be added to both", tags, agentsBlockFields)
	}
}
