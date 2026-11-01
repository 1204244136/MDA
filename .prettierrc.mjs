import * as multilineArrays from "prettier-plugin-multiline-arrays";
import * as maafwSort from "@nekosu/prettier-plugin-maafw-sort";

export default {
    plugins: [maafwSort.patchPlugin(multilineArrays)],
    multilineArraysWrapThreshold: 1,
    // 本地化键序按声明顺序维护。
    maafwLocalePatterns: [],
    maafwInterfacePatterns: [
        "/interface.json",
        "/tasks/.*\\.json",
    ],
    tabWidth: 4,
    printWidth: 120,
    useTabs: false,
    bracketSameLine: true,
    bracketSpacing: false,
    endOfLine: "auto",
    overrides: [
        {
            files: [
                "**/*.yml",
                "**/*.yaml",
            ],
            options: {
                parser: "yaml",
                tabWidth: 2,
            },
        },
        {
            files: ["*.json"],
            options: {
                parser: "json",
                useTabs: false,
                bracketSameLine: false,
            },
        },
    ],
};
