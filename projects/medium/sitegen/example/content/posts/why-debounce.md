---
title: Why file watchers fire five events per save
date: 2026-03-23
tags: [web, tooling]
summary: Editors write temp files, rename, and chmod; wait for the storm to settle.
---
Saving one file in an editor can mean a temp file, a write, a rename and a chmod.
Rebuilding on each event wastes work and races the editor.
The debouncer restarts a short timer on every event and only fires once it is quiet.
