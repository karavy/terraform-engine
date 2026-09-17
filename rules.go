package tfengine

// This file defines the extension point for a resource's "operator rule"
// name (whatever field/label a consumer uses to pick which Terraform
// template mapping applies to a given resource).
//
// TO ADD SUPPORT FOR A NEW OPERATOR RULE:
//  1. Add your Terraform template file (with its <FGT_XXX> placeholders and
//     any <FGT_FOR_STR> loops) wherever your templates live.
//  2. Either register a config-driven rule via LoadOperatorRulesFromFile/
//     LoadOperatorRulesFromBytes/LoadOperatorRulesFromDirectory/
//     LoadOperatorRulesFromSources (see config_driven.go), or register a
//     handcrafted OperatorRuleHandler directly:
//
//       func init() { RegisterOperatorRule("my_new_rule", OperatorRuleFunc(applyMyNewRule)) }
//
// That's it - ApplyOperatorRule (and every other existing file) never
// needs to change.
import (
	"fmt"
)

// OperatorRuleHandler applies one operator-rule's worth of template
// substitutions.
//
// baseFile is the already-copied-and-FOR-loop-expanded (RenderTemplate)
// template file for this resource. spec is whatever JSON-marshalable value
// the caller considers "the resource's configuration" (e.g. a Kubernetes CR
// .Spec) - the engine has no dependency on any specific type. Implementations
// whose resource describes a single instance modify baseFile directly;
// implementations whose resource contains a list (one resource -> many
// Terraform resources) derive one file path per element - see
// IndexedResourceFile in helpers.go - and modify each in turn.
type OperatorRuleHandler interface {
	Apply(resourceName string, spec interface{}, baseFile string) error
}

// OperatorRuleFunc lets a plain function satisfy OperatorRuleHandler
// without declaring a named type for it - the common case for a simple,
// single-purpose rule.
type OperatorRuleFunc func(resourceName string, spec interface{}, baseFile string) error

func (f OperatorRuleFunc) Apply(resourceName string, spec interface{}, baseFile string) error {
	return f(resourceName, spec, baseFile)
}

var operatorRuleRegistry = map[string]OperatorRuleHandler{}

// RegisterOperatorRule makes a new operator-rule name available to
// ApplyOperatorRule. Call it from an init() next to your handler's
// definition, or from your own setup code. Panics on duplicate
// registration - two rules claiming the same name is always a build-time
// mistake, not something worth surviving at runtime (fail fast, at
// startup, rather than silently having the second registration lost).
func RegisterOperatorRule(name string, handler OperatorRuleHandler) {
	if name == "" {
		panic("tfengine: RegisterOperatorRule called with an empty name")
	}
	if _, exists := operatorRuleRegistry[name]; exists {
		panic(fmt.Sprintf("tfengine: operator rule %q registered more than once", name))
	}
	operatorRuleRegistry[name] = handler
}

// KnownOperatorRules returns the currently-registered rule names, mainly
// useful for a clearer error message and for a status/debug endpoint.
func KnownOperatorRules() []string {
	names := make([]string, 0, len(operatorRuleRegistry))
	for name := range operatorRuleRegistry {
		names = append(names, name)
	}
	return names
}

// ApplyOperatorRule looks up ruleName in the registry and applies it to
// baseFile. This is the single dispatch point a consumer needs to call -
// no code here changes to add a new rule (see the package doc above).
func ApplyOperatorRule(ruleName, resourceName string, spec interface{}, baseFile string) error {
	handler, ok := operatorRuleRegistry[ruleName]
	if !ok {
		return fmt.Errorf(
			"operator rule %q non riconosciuta (regole disponibili: %v)",
			ruleName, KnownOperatorRules(),
		)
	}
	return handler.Apply(resourceName, spec, baseFile)
}
