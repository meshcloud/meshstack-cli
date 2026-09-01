package credential

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestValidateRejectsEveryMismatchedPair is what stops a credential nobody selected from
// resolving: naming a method the credential does not hold has to fail here.
func TestValidateRejectsEveryMismatchedPair(t *testing.T) {
	held := map[Method]func() Credential{
		MethodLogin:  func() Credential { return Credential{Login: &Login{}} },
		MethodApiKey: func() Credential { return Credential{ApiKey: &ApiKey{}} },
		MethodManual: func() Credential { return Credential{Manual: &Manual{}} },
	}
	for _, current := range []Method{MethodLogin, MethodApiKey, MethodManual} {
		for present, build := range held {
			c := build()
			c.Current = current
			err := c.Validate()
			if present == current {
				require.NoError(t, err, "%s selecting %s", present, current)
				continue
			}
			require.Error(t, err, "%s selecting %s", present, current)
			assert.Contains(t, err.Error(), string(current))
		}
	}
}

func TestValidateAcceptsTheZeroCredential(t *testing.T) {
	require.NoError(t, Credential{}.Validate())
}

// TestValidateRejectsAMethodWithNoSelection is the other half of "presence is not selection".
// A file holding a method but naming none would otherwise be resolved by whichever caller
// looked at the pointers first.
func TestValidateRejectsAMethodWithNoSelection(t *testing.T) {
	err := Credential{Login: &Login{}, ApiKey: &ApiKey{}}.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "login and apiKey")
}

func TestValidateRejectsAnUnknownMethod(t *testing.T) {
	err := Credential{Current: "kerberos", Login: &Login{}}.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "kerberos")
}

func TestTheConstructorsSelectWhatTheySet(t *testing.T) {
	login := FromLogin(Login{RefreshToken: "r"})
	require.NoError(t, login.Validate())
	assert.Equal(t, MethodLogin, login.Current)
	assert.Equal(t, "r", login.Login.RefreshToken)

	apiKey := FromApiKey(ApiKey{Id: "k"})
	require.NoError(t, apiKey.Validate())
	assert.Equal(t, MethodApiKey, apiKey.Current)
	assert.Equal(t, "k", apiKey.ApiKey.Id)

	manual := FromManual(Manual{})
	require.NoError(t, manual.Validate())
	assert.Equal(t, MethodManual, manual.Current)
	require.NotNil(t, manual.Manual)
}
