package equipmentreroll

import (
	"errors"
	"fmt"
	"strings"

	"github.com/1204244136/MDA/agent/go-service/pkg/i18n"
)

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
	if !c.isSingle() {
		if c.GoalProblem != "" {
			return &goalQuotaError{Problem: c.GoalProblem}
		}
		if c.AllowSubstitution {
			if problem := substitutionProblem(c.Substitution); problem != "" {
				return &goalQuotaError{Problem: problem}
			}
		}
		// 无任何配额的旧配置仍可能依赖调用节点默认值，在最终入口再校验。
		if c.quotaExplicit || len(c.Quota) > 0 {
			if problem := goalQuotaProblem(c.Quota); problem != "" {
				return &goalQuotaError{Problem: problem}
			}
		}
	}
	return nil
}

type goalQuotaError struct {
	Problem string
}

func (e *goalQuotaError) Error() string {
	return fmt.Sprintf("invalid equipment reroll target: %s", e.Problem)
}

func rerollConfigErrorMessage(err error) string {
	var goalError *goalQuotaError
	if errors.As(err, &goalError) {
		if goalError.Problem == "empty quota" {
			return i18n.T("tasker.equipment_reroll.goal_empty")
		}
		if strings.HasPrefix(goalError.Problem, "substitution:") {
			return i18n.T("tasker.equipment_reroll.substitution_invalid")
		}
		return i18n.T("tasker.equipment_reroll.goal_invalid")
	}
	return i18n.T("tasker.equipment_reroll.config_invalid")
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
