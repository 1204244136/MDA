<!-- markdownlint-disable MD033 MD041 -->
<p align="center">
  <img alt="LOGO" src="https://s1.imagehub.cc/images/2026/05/12/d1d0730a19f251d8ea800897754f0ab2.png" width="256" height="256" />
</p>

<div align="center">

# MDA

Maa Doro Assistant

**[简体中文](README.md)** | **English**

</div>

<p align="center">
  <img alt="Go" style="display:inline-block" src="https://img.shields.io/badge/Go-00ADD8?logo=go&logoColor=white">
  <img alt="MaaFramework" style="display:inline-block" src="https://img.shields.io/badge/MaaFramework-%2300BFFF">
  <img alt="platform" style="display:inline-block" src="https://img.shields.io/badge/platform-Windows%20%7C%20macOS%20%7C%20Linux-blueviolet">
  <img alt="license" style="display:inline-block" src="https://img.shields.io/github/license/1204244136/MDA">
  <br>
  <img alt="release" style="display:inline-block" src="https://img.shields.io/github/v/release/1204244136/MDA">
  <img alt="commit" style="display:inline-block" src="https://img.shields.io/github/commit-activity/m/1204244136/MDA">
  <img alt="stars" style="display:inline-block" src="https://img.shields.io/github/stars/1204244136/MDA?style=social">
  <img alt="downloads" style="display:inline-block" src="https://img.shields.io/github/downloads/1204244136/MDA/total?style=social">
  <a href="https://mirrorchyan.com/zh/projects?rid=MDA&os=windows&arch=x64&channel=stable&source=mdagh-badge-en" target="_blank"><img alt="mirrorc" style="display:inline-block" src="https://img.shields.io/badge/Mirror%E9%85%B1-%239af3f6?logo=countingworkspro&logoColor=4f46e5"></a>
</p>

MDA is a game automation assistant built on [MaaFramework](https://github.com/MaaXYZ/MaaFramework), rewritten from [DoroHelper](https://github.com/1204244136/DoroHelper). It automates daily routines and event content in the game, saving you time and effort.

---

## ✨ Features

MDA includes a variety of tasks covering dailies, events, and utilities — all of them can be toggled on in the app:

### Pre-task Flow

- 🚪 **Enter Hall**: Return to the game hall to provide a consistent starting point for all subsequent tasks.

### Daily Tasks

- 📅 **Daily Rewards**: Claim friend points, mail, missions, Pass and other daily rewards in one go.
- 🏠 **Outpost**: Claim defense rewards and complete dispatch board and brief encounter tasks.
- 🛒 **Shop**: Buy what you need in the common, arena, and recycling shops.
- 💎 **Cash Shop**: Enter the cash shop and claim free packages and other rewards.
- 🧪 **Simulation Room**: Automatically complete normal / overclocked simulation room battles.
- ⚔️ **Arena**: Battle in the rookie, special, and champion arenas and claim accumulated rewards.
- 🗼 **Tribe Tower**: Automatically challenge the tribe towers of each faction.
- 🎯 **Interception**: Automatically challenge normal / anomaly interception battles and claim rewards.
- 💬 **Advise**: Automatically advise Nikkes and claim bond and episode rewards.

### Periodic Tasks

- 🎪 **Large Event**: Automatically handle login stamps, challenges, story, missions and mini-games in large events (events with SD characters).
- 🎫 **Small Event**: Automatically handle challenges, story and missions in small events (events without SD characters).
- 🔥 **Solo Raid**: Automatically complete solo raid stage challenges or quick battles.
- 🤝 **Coordinated Operations**: Automatically complete coordinated operations and claim rewards.

### Utilities

- 🎁 **Open Lucky Boxes**: Automatically open various boxes from the inventory.
- 📈 **Account Nurturing**: Automatically perform breakthroughs, synchro device enhancements and other nurturing operations.
- 🔨 **Effect Reroll**: Automatically reroll effects on T10 equipment, with Character and Single modes.
- 🗺️ **Auto Map Pushing**: Automatically click monsters to fight and trigger mechanisms to push through main stages.
- 🔴 **Clear Red Dots**: Automatically clear red-dot notifications across supported interfaces.

---

## Getting Started

### 1. First Launch

Take a moment to explore the interface before running any tasks to understand the available features and settings.

### 2. Set Up Hotkeys (Recommended)

Go to **Settings (top-right corner) → Hotkeys** and enable global hotkeys, in case the program becomes unresponsive and you need to exit.

---

## Language Compatibility

MDA's interface supports multiple languages including Chinese and English, but **the script's functionality is currently only adapted for the Chinese game interface**.

If you are using an English or other language game interface, you may encounter recognition errors or functional issues. If you experience errors, please switch your game to **Simplified Chinese** first and try again. If the problem persists after switching, feel free to submit feedback and we'll help investigate.

---

## Related Projects

[BlablalinkTasker](https://github.com/1204244136/BlablalinkTasker) is a daily task automation tool for the Blablalink / NIKKE community, supporting check-ins, likes, browsing and reward redemption. After the initial login setup, you can call its `日常运行.bat` through MXU's **Special Tasks → Custom Programs** to run it together with MDA.

---

## ⭐ Star History

<a href="https://www.star-history.com/#1204244136/MDA&Date">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="https://api.star-history.com/svg?repos=1204244136/MDA&type=Date&theme=dark" />
    <source media="(prefers-color-scheme: light)" srcset="https://api.star-history.com/svg?repos=1204244136/MDA&type=Date" />
    <img alt="Star History Chart" src="https://api.star-history.com/svg?repos=1204244136/MDA&type=Date" />
  </picture>
</a>

---

## Reporting Issues

If the script encounters an error, follow these steps to collect information for troubleshooting.

### Step 1: Enable Debug Images

1. Go to **Settings → Debug**
2. Enable **Save Debug Images**

> ⚠️ This option must be re-enabled each time you start the program.

### Step 2: Reproduce the Issue

Debug mode saves a screenshot for every action, so **avoid running tasks for extended periods** — it will generate a large number of images and consume disk space.

Recommended approach:

- After enabling debug mode, **only run the task that has the issue**
- Stop immediately after reproducing the problem and prepare to package the logs

### Step 3: Export the Logs

1. Click the **Export Logs** icon next to **Run Log** in the bottom-right corner
2. In the `debug` folder that opens, find the generated archive
3. Send the archive to the developer

> 💡 After submitting feedback, it's recommended to **delete the old `vision` folder** and restart the program. This keeps debug images from different issues separate and makes troubleshooting easier.
