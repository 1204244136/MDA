package equipmentreroll

import "fmt"

// rerollOperation 描述重洗内容，与角色/单件范围无关。
type rerollOperation string

const (
	rerollOperationEffect rerollOperation = "effect"
	rerollOperationValue  rerollOperation = "value"
)

func (c carrierConfig) isValue() bool { return c.Operation == rerollOperationValue }

func (c carrierConfig) validateOperation() error {
	if c.ConfigProblem != "" {
		return fmt.Errorf("invalid config: %s", c.ConfigProblem)
	}
	if c.Operation != "" && c.Operation != rerollOperationEffect && c.Operation != rerollOperationValue {
		return fmt.Errorf("unknown operation %q", c.Operation)
	}
	if c.Mode != rerollModeCharacter && c.Mode != rerollModeSingle {
		return fmt.Errorf("unknown mode %q", c.Mode)
	}
	if c.isValue() {
		if c.isSingle() && !isEquipmentPart(c.Part) {
			return fmt.Errorf("invalid part %q", c.Part)
		}
		return c.ValueTargets.validate(15 * len(c.valueScope()))
	}
	return nil
}

// rerollDecisionTarget 是扫描后的业务分发点，供后续结果返回链复用。
func rerollDecisionTarget(cfg carrierConfig) string {
	if cfg.isValue() {
		return "EquipmentRerollValueDecide"
	}
	if cfg.isSingle() {
		return "EquipmentRerollSingleDecide"
	}
	return "EquipmentRerollDecide"
}
