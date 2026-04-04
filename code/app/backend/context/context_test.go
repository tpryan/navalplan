package context

import (
	"context"
	"testing"

	"app/models"

	"github.com/stretchr/testify/assert"
)

func TestPersonContext(t *testing.T) {
	t.Run("Add and Get Person", func(t *testing.T) {
		person := &models.Person{
			ID:    1,
			Email: "test@example.com",
			Name:  "Test User",
		}

		ctx := context.Background()
		ctx = AddPersonToContext(ctx, person)

		retrievedPerson := GetPersonFromContext(ctx)
		assert.NotNil(t, retrievedPerson)
		assert.Equal(t, person, retrievedPerson)
	})

	t.Run("Get Person from empty context", func(t *testing.T) {
		ctx := context.Background()
		retrievedPerson := GetPersonFromContext(ctx)
		assert.Nil(t, retrievedPerson)
	})
}
