"""构建并运行不触碰实际设备的实验性输入核心和加载器测试。"""

import argparse
import shutil
import struct
import subprocess
import tempfile
from pathlib import Path

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[2]


def run(*args):
    subprocess.run([str(x) for x in args], check=True, cwd=ROOT)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--x86-dll', type=Path, help='可选：用 x86 编译的 fake_connector.dll')
    args = parser.parse_args()
    if not shutil.which('gcc') or not shutil.which('g++'):
        raise SystemExit('需要本机已有的 Windows x64 gcc / g++，不会自动安装')
    with tempfile.TemporaryDirectory(dir=ROOT) as temporary:
        out = Path(temporary).resolve()
        if not out.is_relative_to(ROOT.resolve()):
            raise RuntimeError('测试目录必须位于项目内')
        (out / 'config').mkdir()
        fake = HERE / 'fake_connector.c'
        variants = (
            ('good.dll', '1', '1', []), ('missing.dll', '1', '1', ['-DFAKE_MISSING_WHEEL']),
            ('version.dll', '2', '1', []), ('open-fail.dll', '1', '0', []),
        )
        for name, version, opened, flags in variants:
            run('gcc', '-shared', '-DFAKE_VERSION=' + version, '-DFAKE_OPEN=' + opened,
                *flags, fake, '-o', out / name)
        malformed = bytearray((out / 'good.dll').read_bytes())
        offset = struct.unpack_from('<I', malformed, 0x3c)[0]
        if malformed[offset:offset + 4] != b'PE\0\0':
            raise RuntimeError('模拟 DLL 不是 PE 文件')
        struct.pack_into('<H', malformed, offset + 4, 0x014c)
        (out / 'bad-machine.dll').write_bytes(malformed)
        if args.x86_dll:
            shutil.copy2(args.x86_dll, out / 'x86.dll')
        run('g++', '-std=c++20', '-Wall', '-Wextra', '-Werror', HERE / 'core_test.cpp', '-o', out / 'core_test.exe')
        run('g++', '-std=c++20', '-Wall', '-Wextra', '-Wno-cast-function-type', '-static-libgcc', '-static-libstdc++',
            HERE / 'library_test.cpp', '-o', out / 'library_test.exe')
        run(out / 'core_test.exe')
        run(out / 'library_test.exe', out)
        run('python', '-m', 'unittest', 'discover', '-s', HERE, '-p', '*_test.py', '-v')
    print('实验性输入模拟测试通过；未加载真实连接器或发送输入。')


if __name__ == '__main__':
    main()
