package runtimeidentity

import (
	"fmt"

	"github.com/JeremyDev87/theseus/internal/canonical"
	"github.com/JeremyDev87/theseus/internal/model"
)

func Create(value model.RuntimeIdentity) (model.RuntimeIdentity, error) {
	payload := map[string]any{
		"kind":                 value.Kind,
		"binName":              value.BinName,
		"executablePath":       value.ExecutablePath,
		"executableRealPath":   value.ExecutableRealPath,
		"optionalDependencies": value.OptionalDependencies,
	}
	switch value.Kind {
	case "source":
	case "installed":
		payload["packageName"] = value.PackageName
		payload["packageVersion"] = value.PackageVersion
	default:
		return model.RuntimeIdentity{}, fmt.Errorf("runtime kind must be installed or source")
	}
	canonicalValue, err := canonical.Marshal(payload)
	if err != nil {
		return model.RuntimeIdentity{}, err
	}
	value.Canonical = canonicalValue
	value.SHA256 = canonical.SHA256String(canonicalValue)
	return value, nil
}
