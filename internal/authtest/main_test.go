package authtest_test

import (
	"os"
	"testing"

	"github.com/meros/fyr/internal/authtest"
)

func TestMain(m *testing.M) { os.Exit(authtest.Run(m)) }
