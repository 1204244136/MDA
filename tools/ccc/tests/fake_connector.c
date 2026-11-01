#define CCC_BUILD_DLL
#include "../ccc_api.h"

static int opens, closes;

__declspec(dllexport) uint32_t CCC_CALL ccc_api_version(void) { return FAKE_VERSION; }
__declspec(dllexport) int32_t CCC_CALL ccc_open(void) { ++opens; return FAKE_OPEN; }
__declspec(dllexport) void CCC_CALL ccc_close(void) { ++closes; }
__declspec(dllexport) int32_t CCC_CALL ccc_mouse_move(int32_t x, int32_t y) { (void)x; (void)y; return 1; }
__declspec(dllexport) int32_t CCC_CALL ccc_mouse_button(int32_t button, int32_t down) { (void)button; (void)down; return 1; }
__declspec(dllexport) int32_t CCC_CALL ccc_key(int32_t vk, int32_t down) { (void)vk; (void)down; return 1; }
#ifndef FAKE_MISSING_WHEEL
__declspec(dllexport) int32_t CCC_CALL ccc_wheel(int32_t delta) { (void)delta; return 1; }
#endif
__declspec(dllexport) int32_t CCC_CALL ccc_release_all(void) { return 1; }
__declspec(dllexport) int CCC_CALL mock_open_count(void) { return opens; }
__declspec(dllexport) int CCC_CALL mock_close_count(void) { return closes; }
