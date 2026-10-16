# Pipeline Debug Rules Reference

## Critical Issues

### Missing Required Fields

**Rule**: Every node must have either `recognition` (V1) or `type` (V2) to specify what it matches.

**Detection**: Check if node has `recognition` or `type` property.

**Fix**: Add appropriate recognition type based on what the node should match.

```json
// ❌ Wrong: Node without recognition
{
    "MyNode": {
        "next": ["NextNode"]
    }
}

// ✅ Correct: Node with recognition
{
    "MyNode": {
        "recognition": "TemplateMatch",
        "template": "my_template.png",
        "next": ["NextNode"]
    }
}
```

### Invalid Recognition Parameters

**Rule**: Each recognition type has specific required parameters.

**Detection**: Check if required parameters for the recognition type are present.

**Common Issues**:

- `TemplateMatch`: Missing `template` parameter
- `OCR`: Missing `expected` parameter (optional but recommended)
- `ColorMatch`: Missing `lower` and `upper` parameters
- `FeatureMatch`: Missing `template` parameter

**Fix**: Add the required parameters for the recognition type.

```json
// ❌ Wrong: TemplateMatch without template
{
    "MyNode": {
        "recognition": "TemplateMatch",
        "next": ["NextNode"]
    }
}

// ✅ Correct: TemplateMatch with template
{
    "MyNode": {
        "recognition": "TemplateMatch",
        "template": "my_template.png",
        "next": ["NextNode"]
    }
}
```

### Invalid Next References

**Rule**: All nodes referenced in `next[]`, `interrupt[]`, `sub[]` must exist in the pipeline.

**Detection**: Check if all referenced node names exist as keys in the pipeline.

**Fix**: Either add the missing node or correct the reference.

```json
// ❌ Wrong: Reference to non-existent node
{
    "Start": {
        "next": ["NonExistentNode"]
    }
}

// ✅ Correct: Reference to existing node
{
    "Start": {
        "next": ["ExistingNode"]
    },
    "ExistingNode": {
        "recognition": "DirectHit"
    }
}
```

### Circular Dependencies

**Rule**: Avoid infinite loops in `next` chains unless intentional.

**Detection**: Traverse `next` references and detect cycles.

**Fix**: Break the cycle by adding a termination condition or using `interrupt`.

```json
// ❌ Wrong: Infinite loop
{
    "A": {"next": ["B"]},
    "B": {"next": ["A"]}
}

// ✅ Correct: Loop with termination
{
    "A": {
        "next": ["B"],
        "interrupt": ["Exit"]
    },
    "B": {"next": ["A"]},
    "Exit": {
        "recognition": "TemplateMatch",
        "template": "exit_condition.png"
    }
}
```

## Warnings

### Unreferenced Nodes (Orphans)

**Rule**: Every node should be reachable from some entry point.

**Detection**: Find nodes that are never referenced in any `next[]`, `interrupt[]`, or `sub[]`.

**Fix**: Either reference the node or remove it if unused.

```json
// ❌ Warning: Orphan node
{
    "Start": {"next": ["End"]},
    "End": {"recognition": "DirectHit"},
    "Orphan": {"recognition": "TemplateMatch", "template": "unused.png"}
}

// ✅ Correct: All nodes referenced
{
    "Start": {"next": ["Middle", "End"]},
    "Middle": {
        "recognition": "TemplateMatch",
        "template": "condition.png",
        "next": ["End"]
    },
    "End": {"recognition": "DirectHit"}
}
```

### Dead End Nodes

**Rule**: Nodes without `next` should be intentional terminal nodes.

**Detection**: Find nodes with no `next` property that aren't obvious terminal actions.

**Fix**: Add `next` to continue execution or mark as intentional terminal.

```json
// ❌ Warning: Unexpected dead end
{
    "Start": {
        "recognition": "TemplateMatch",
        "template": "button.png",
        "next": ["Process"]
    },
    "Process": {
        "action": "Click"
        // No next - execution stops here unexpectedly
    }
}

// ✅ Correct: Explicit terminal or continuation
{
    "Start": {
        "recognition": "TemplateMatch",
        "template": "button.png",
        "next": ["Process"]
    },
    "Process": {
        "action": "Click",
        "next": ["Complete"]
    },
    "Complete": {
        "recognition": "DirectHit",
        "desc": "Terminal node - process complete"
    }
}
```

### Missing Description

**Rule**: Add `desc` to complex nodes for maintainability.

**Detection**: Check if nodes have `desc` property.

**Fix**: Add descriptive text explaining node purpose.

```json
// ❌ Warning: No description
{
    "ComplexLogic": {
        "recognition": "TemplateMatch",
        "template": "complex_condition.png",
        "next": ["BranchA", "BranchB"],
        "interrupt": ["ErrorHandler"]
    }
}

// ✅ Correct: With description
{
    "ComplexLogic": {
        "recognition": "TemplateMatch",
        "template": "complex_condition.png",
        "next": ["BranchA", "BranchB"],
        "interrupt": ["ErrorHandler"],
        "desc": "Main decision point - checks current state and branches accordingly"
    }
}
```

## Suggestions

### Performance Optimization

#### 1. Optimize ROI

**Issue**: Large recognition areas slow down matching.

**Suggestion**: Use smaller, focused ROI areas when possible.

```json
// ❌ Slow: Full screen recognition
{
    "FindButton": {
        "recognition": "TemplateMatch",
        "template": "button.png",
        "roi": [0, 0, 1280, 720]
    }
}

// ✅ Faster: Focused ROI
{
    "FindButton": {
        "recognition": "TemplateMatch",
        "template": "button.png",
        "roi": [100, 500, 200, 100]  // Only search where button appears
    }
}
```

#### 2. Optimize Template Matching

**Issue**: Template matching can be slow with large templates or high-resolution images.

**Suggestion**:

- Use smaller template images when possible
- Adjust `threshold` to reduce false positives
- Consider using `FeatureMatch` for complex scenes

```json
// ❌ Slow: Large template, high threshold
{
    "MatchLogo": {
        "recognition": "TemplateMatch",
        "template": "large_logo.png",
        "threshold": 0.99
    }
}

// ✅ Faster: Optimized template and threshold
{
    "MatchLogo": {
        "recognition": "TemplateMatch",
        "template": "logo_small.png",
        "threshold": 0.8,
        "roi": [50, 50, 200, 100]
    }
}
```

#### 3. Reduce Unnecessary Nodes

**Issue**: Too many nodes increase execution time and complexity.

**Suggestion**: Combine related operations when possible.

```json
// ❌ Verbose: Multiple nodes for simple sequence
{
    "Step1": {
        "recognition": "TemplateMatch",
        "template": "button1.png",
        "next": ["Step2"]
    },
    "Step2": {
        "action": "Click",
        "next": ["Step3"]
    },
    "Step3": {
        "recognition": "TemplateMatch",
        "template": "button2.png",
        "next": ["Step4"]
    },
    "Step4": {
        "action": "Click"
    }
}

// ✅ Concise: Combined operations
{
    "ClickButton1": {
        "recognition": "TemplateMatch",
        "template": "button1.png",
        "action": "Click",
        "next": ["ClickButton2"]
    },
    "ClickButton2": {
        "recognition": "TemplateMatch",
        "template": "button2.png",
        "action": "Click"
    }
}
```

### Reliability Improvement

#### 1. Add Error Handling

**Issue**: Nodes may fail unexpectedly.

**Suggestion**: Use `on_error` to handle failures gracefully.

```json
// ❌ Fragile: No error handling
{
    "RiskyOperation": {
        "recognition": "TemplateMatch",
        "template": "may_not_exist.png",
        "next": ["Success"]
    }
}

// ✅ Robust: With error handling
{
    "RiskyOperation": {
        "recognition": "TemplateMatch",
        "template": "may_not_exist.png",
        "next": ["Success"],
        "on_error": ["ErrorHandler"]
    },
    "ErrorHandler": {
        "action": "Click",
        "target": [640, 360],  // Click somewhere safe
        "next": ["RetryOrSkip"]
    }
}
```

#### 2. Improve Recognition Accuracy

**Issue**: Template matching may be unreliable.

**Suggestion**:

- Use multiple recognition methods
- Adjust thresholds based on testing
- Consider using `FeatureMatch` for variable scenes

```json
// ❌ Unreliable: Single recognition method
{
    "FindItem": {
        "recognition": "TemplateMatch",
        "template": "item.png",
        "threshold": 0.7
    }
}

// ✅ More reliable: Multiple methods or better parameters
{
    "FindItem": {
        "recognition": "FeatureMatch",
        "template": "item.png",
        "count": 10,
        "ratio": 0.7
    }
}
```

### Maintainability Improvement

#### 1. Use Meaningful Names

**Issue**: Cryptic node names make debugging difficult.

**Suggestion**: Use descriptive names that explain purpose.

```json
// ❌ Poor: Cryptic names
{
    "N1": {"next": ["N2"]},
    "N2": {"next": ["N3"]},
    "N3": {}
}

// ✅ Good: Descriptive names
{
    "CheckMainMenu": {"next": ["NavigateToSettings"]},
    "NavigateToSettings": {"next": ["VerifySettingsLoaded"]},
    "VerifySettingsLoaded": {}
}
```

#### 2. Group Related Nodes

**Issue**: Flat structure makes logic hard to follow.

**Suggestion**: Use prefixes or comments to group related nodes.

```json
// ❌ Flat: Hard to follow
{
    "Start": {"next": ["Login", "Register"]},
    "Login": {"next": ["Dashboard"]},
    "Register": {"next": ["Dashboard"]},
    "Dashboard": {}
}

// ✅ Grouped: Clear structure
{
    "Auth_Start": {"next": ["Auth_Login", "Auth_Register"]},
    "Auth_Login": {"next": ["App_Dashboard"]},
    "Auth_Register": {"next": ["App_Dashboard"]},
    "App_Dashboard": {}
}
```

## Validation Checklist

Use this checklist when debugging pipeline snippets:

- [ ] All nodes have `recognition` or `type` defined
- [ ] All recognition types have required parameters
- [ ] All `next[]`, `interrupt[]`, `sub[]` references point to existing nodes
- [ ] No unintended circular dependencies
- [ ] No orphan nodes (unreachable)
- [ ] No unexpected dead ends
- [ ] ROI values are reasonable (not too large/small)
- [ ] Threshold values are appropriate for the use case
- [ ] Complex nodes have `desc` documentation
- [ ] Node names are meaningful and descriptive
- [ ] Error handling is present for risky operations
- [ ] Performance-critical paths are optimized
