# Common Pipeline Patterns

## Basic Patterns

### 1. Simple Click Action

Click a button when it appears.

```json
{
    "ClickButton": {
        "recognition": "TemplateMatch",
        "template": "button.png",
        "action": "Click",
        "next": ["NextStep"]
    }
}
```

### 2. Wait for Condition

Wait until a specific element appears.

```json
{
    "WaitForElement": {
        "recognition": "TemplateMatch",
        "template": "element.png",
        "threshold": 0.8,
        "next": ["Proceed"]
    },
    "Proceed": {
        "action": "Click",
        "target": [
            640,
            360
        ]
    }
}
```

### 3. Conditional Branching

Branch based on what's on screen.

```json
{
    "CheckState": {
        "recognition": "TemplateMatch",
        "template": "state_a.png",
        "next": ["HandleStateA"],
        "interrupt": ["CheckStateB"]
    },
    "CheckStateB": {
        "recognition": "TemplateMatch",
        "template": "state_b.png",
        "next": ["HandleStateB"]
    },
    "HandleStateA": {
        "action": "Click",
        "target": [
            100,
            200
        ]
    },
    "HandleStateB": {
        "action": "Click",
        "target": [
            300,
            400
        ]
    }
}
```

## Navigation Patterns

### 4. Menu Navigation

Navigate through nested menus.

```json
{
    "MainMenu": {
        "recognition": "TemplateMatch",
        "template": "main_menu.png",
        "next": ["ClickSettings"]
    },
    "ClickSettings": {
        "recognition": "TemplateMatch",
        "template": "settings_button.png",
        "action": "Click",
        "next": ["WaitForSettings"]
    },
    "WaitForSettings": {
        "recognition": "TemplateMatch",
        "template": "settings_page.png",
        "next": ["AdjustSettings"]
    }
}
```

### 5. Page Navigation with Back

Navigate forward and handle back navigation.

```json
{
    "Page1": {
        "recognition": "TemplateMatch",
        "template": "page1.png",
        "next": ["GoToPage2"]
    },
    "GoToPage2": {
        "recognition": "TemplateMatch",
        "template": "next_button.png",
        "action": "Click",
        "next": ["Page2"]
    },
    "Page2": {
        "recognition": "TemplateMatch",
        "template": "page2.png",
        "next": ["DoSomething"],
        "interrupt": ["BackButton"]
    },
    "BackButton": {
        "recognition": "TemplateMatch",
        "template": "back_button.png",
        "action": "Click",
        "next": ["Page1"]
    }
}
```

## Error Handling Patterns

### 6. Retry Pattern

Retry an operation until it succeeds.

```json
{
    "RetryOperation": {
        "recognition": "TemplateMatch",
        "template": "operation_button.png",
        "action": "Click",
        "next": ["CheckSuccess"],
        "on_error": ["WaitAndRetry"]
    },
    "CheckSuccess": {
        "recognition": "TemplateMatch",
        "template": "success_indicator.png",
        "next": ["Complete"]
    },
    "WaitAndRetry": {
        "action": "Sleep",
        "ms": 1000,
        "next": ["RetryOperation"]
    }
}
```

### 7. Timeout Pattern

Give up after a certain time.

```json
{
    "StartWait": {
        "recognition": "DirectHit",
        "next": ["WaitForElement"],
        "desc": "Start waiting, record start time"
    },
    "WaitForElement": {
        "recognition": "TemplateMatch",
        "template": "element.png",
        "next": ["Success"],
        "interrupt": ["CheckTimeout"]
    },
    "CheckTimeout": {
        "recognition": "TemplateMatch",
        "template": "timeout_indicator.png",
        "next": ["HandleTimeout"],
        "desc": "Check if we've waited too long"
    },
    "HandleTimeout": {
        "action": "Click",
        "target": [
            640,
            360
        ],
        "next": ["FailGracefully"]
    }
}
```

## Advanced Patterns

### 8. Multi-State Handler

Handle multiple possible states.

```json
{
    "DetectState": {
        "recognition": "TemplateMatch",
        "template": "state_loading.png",
        "next": ["WaitForLoaded"],
        "interrupt": [
            "CheckState2",
            "CheckState3"
        ]
    },
    "CheckState2": {
        "recognition": "TemplateMatch",
        "template": "state_error.png",
        "next": ["HandleError"]
    },
    "CheckState3": {
        "recognition": "TemplateMatch",
        "template": "state_ready.png",
        "next": ["Proceed"]
    },
    "WaitForLoaded": {
        "recognition": "TemplateMatch",
        "template": "state_ready.png",
        "next": ["Proceed"]
    },
    "HandleError": {
        "action": "Click",
        "target": [
            640,
            360
        ],
        "next": ["DetectState"]
    }
}
```

### 9. Parallel Actions

Execute multiple actions simultaneously.

```json
{
    "StartParallel": {
        "recognition": "DirectHit",
        "sub": [
            "Action1",
            "Action2"
        ],
        "next": ["WaitForBoth"]
    },
    "Action1": {
        "recognition": "TemplateMatch",
        "template": "button1.png",
        "action": "Click"
    },
    "Action2": {
        "recognition": "TemplateMatch",
        "template": "button2.png",
        "action": "Click"
    },
    "WaitForBoth": {
        "recognition": "TemplateMatch",
        "template": "both_done.png",
        "next": ["Continue"]
    }
}
```

### 10. Loop with Counter

Repeat an action a specific number of times.

```json
{
    "StartLoop": {
        "recognition": "DirectHit",
        "next": ["LoopBody"],
        "desc": "Initialize counter (use pipeline_override for actual counter)"
    },
    "LoopBody": {
        "recognition": "TemplateMatch",
        "template": "item_to_process.png",
        "action": "Click",
        "next": ["CheckCounter"]
    },
    "CheckCounter": {
        "recognition": "TemplateMatch",
        "template": "continue_condition.png",
        "next": ["LoopBody"],
        "interrupt": ["ExitLoop"]
    },
    "ExitLoop": {
        "recognition": "DirectHit",
        "next": ["Complete"]
    }
}
```

## OCR Patterns

### 11. Text Recognition and Validation

Read text and validate it.

```json
{
    "ReadText": {
        "recognition": "OCR",
        "expected": "\\d{4}-\\d{2}-\\d{2}", // Date pattern
        "roi": [
            100,
            100,
            200,
            50
        ],
        "next": ["ProcessText"]
    },
    "ProcessText": {
        "action": "Click",
        "target": [
            640,
            360
        ]
    }
}
```

### 12. Dynamic Text Handling

Handle text that changes.

```json
{
    "ReadDynamicText": {
        "recognition": "OCR",
        "expected": ".*", // Match any text
        "roi": [
            100,
            100,
            300,
            50
        ],
        "next": ["CheckTextContent"]
    },
    "CheckTextContent": {
        "recognition": "OCR",
        "expected": "Success|Complete|Done",
        "roi": [
            100,
            100,
            300,
            50
        ],
        "next": ["HandleSuccess"],
        "interrupt": ["HandleOtherText"]
    }
}
```

## Color Matching Patterns

### 13. Color-Based Decision

Make decisions based on color presence.

```json
{
    "CheckColor": {
        "recognition": "ColorMatch",
        "method": 4, // RGB
        "lower": [
            0,
            200,
            0
        ], // Green lower bound
        "upper": [
            100,
            255,
            100
        ], // Green upper bound
        "count": 100,
        "next": ["GreenDetected"],
        "interrupt": ["NoGreen"]
    },
    "GreenDetected": {
        "action": "Click",
        "target": [
            640,
            360
        ]
    },
    "NoGreen": {
        "action": "Sleep",
        "ms": 1000,
        "next": ["CheckColor"]
    }
}
```

## Feature Matching Patterns

### 14. Robust Element Detection

Use feature matching for variable scenes.

```json
{
    "FindElement": {
        "recognition": "FeatureMatch",
        "template": "element.png",
        "count": 15,
        "ratio": 0.7,
        "detector": "SIFT",
        "next": ["InteractWithElement"]
    },
    "InteractWithElement": {
        "action": "Click",
        "target": [
            640,
            360
        ]
    }
}
```

## Best Practices Summary

1. **Name nodes descriptively**: Use names that explain purpose
2. **Add documentation**: Use `desc` for complex logic
3. **Handle errors**: Use `on_error` for risky operations
4. **Optimize ROI**: Use focused recognition areas
5. **Set appropriate thresholds**: Balance accuracy vs. reliability
6. **Avoid deep nesting**: Keep node chains reasonable length
7. **Use interrupts wisely**: For conditional branching
8. **Test thoroughly**: Verify all paths work as expected
