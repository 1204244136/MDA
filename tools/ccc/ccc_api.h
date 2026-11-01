#pragma once

#include <stdint.h>

/* CCC v1: Windows x64, C ABI, __cdecl, fixed-width arguments. */
#if defined(_WIN32)
#define CCC_CALL __cdecl
#if defined(CCC_BUILD_DLL)
#define CCC_EXPORT __declspec(dllexport)
#else
#define CCC_EXPORT
#endif
#else
#define CCC_CALL
#define CCC_EXPORT
#endif

#ifdef __cplusplus
extern "C" {
#endif

/* Each function except close returns 1 on success and 0 on failure. */
CCC_EXPORT uint32_t CCC_CALL ccc_api_version(void); /* Must return 1. */
CCC_EXPORT int32_t CCC_CALL ccc_open(void);
CCC_EXPORT void CCC_CALL ccc_close(void); /* Also valid after an unsuccessful open. */
CCC_EXPORT int32_t CCC_CALL ccc_mouse_move(int32_t dx, int32_t dy); /* Relative counts. */
CCC_EXPORT int32_t CCC_CALL ccc_mouse_button(int32_t button, int32_t down); /* 0 left, 1 right, 2 middle. */
CCC_EXPORT int32_t CCC_CALL ccc_key(int32_t vk, int32_t down); /* Windows virtual-key code, 1..254. */
CCC_EXPORT int32_t CCC_CALL ccc_wheel(int32_t delta); /* Vertical WHEEL_DELTA units. */
CCC_EXPORT int32_t CCC_CALL ccc_release_all(void);

#ifdef __cplusplus
}
#endif
