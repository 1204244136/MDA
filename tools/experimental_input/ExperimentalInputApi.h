#pragma once

#include <stdint.h>

/* Experimental input connector ABI v1: Windows x64, C ABI, __cdecl, fixed-width arguments. */
#if defined(_WIN32)
#define EXPERIMENTAL_INPUT_CALL __cdecl
#if defined(EXPERIMENTAL_INPUT_BUILD_DLL)
#define EXPERIMENTAL_INPUT_EXPORT __declspec(dllexport)
#else
#define EXPERIMENTAL_INPUT_EXPORT
#endif
#else
#define EXPERIMENTAL_INPUT_CALL
#define EXPERIMENTAL_INPUT_EXPORT
#endif

#ifdef __cplusplus
extern "C" {
#endif

/* Each function except close returns 1 on success and 0 on failure. */
EXPERIMENTAL_INPUT_EXPORT uint32_t EXPERIMENTAL_INPUT_CALL ccc_api_version(void); /* Must return 1. */
EXPERIMENTAL_INPUT_EXPORT int32_t EXPERIMENTAL_INPUT_CALL ccc_open(void);
EXPERIMENTAL_INPUT_EXPORT void EXPERIMENTAL_INPUT_CALL ccc_close(void); /* Also valid after an unsuccessful open. */
EXPERIMENTAL_INPUT_EXPORT int32_t EXPERIMENTAL_INPUT_CALL ccc_mouse_move(int32_t dx, int32_t dy); /* Relative counts. */
EXPERIMENTAL_INPUT_EXPORT int32_t EXPERIMENTAL_INPUT_CALL ccc_mouse_button(int32_t button, int32_t down); /* 0 left, 1 right, 2 middle. */
EXPERIMENTAL_INPUT_EXPORT int32_t EXPERIMENTAL_INPUT_CALL ccc_key(int32_t vk, int32_t down); /* Windows virtual-key code, 1..254. */
EXPERIMENTAL_INPUT_EXPORT int32_t EXPERIMENTAL_INPUT_CALL ccc_wheel(int32_t delta); /* Vertical WHEEL_DELTA units. */
EXPERIMENTAL_INPUT_EXPORT int32_t EXPERIMENTAL_INPUT_CALL ccc_release_all(void);

#ifdef __cplusplus
}
#endif
