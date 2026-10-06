# Rampart-Refortified
A fan mod/custom emulator for the DOS version of Rampart (1992)

RAMPART: REFORTIFIED  9.0
A mod by Shifting Dollars
Official source: https://github.com/SHIFTINGDOLLARS

Unofficial fan mod. Rampart (c) 1990 Atari Games; DOS version by Bitmasters.
This package contains NO game files. You need your own copy of DOS Rampart. SNES/MAME/Other versions will not work.


WHAT IT IS
----------
Rampart.exe is a standalone Windows player for DOS Rampart. It runs the
original game unchanged inside its own built-in emulator and adds some new features:

  - Computer bot players (Easy / Normal / Hard), up to a full 3-bot game
  - Endless mode with a top-10 records board
  - Smart Pieces Toggle (rule: you get the piece your wall needs. Intended for multiplayer to avoid cheap shots.)
  - Special cannons which can be toggled outside Enhanced Mode: balloons/super cannons 
  - A custom cannon called the Grunt Buster that only fires at grunts.
  - Castle color presets, game rules, sound options
  - Xbox / XInput gamepad support
  - F12 options menu (settings, controls, rules, About)

No DOSBox needed. Windows 10 or 11, 64-bit. (Linux / Steam Deck: should run
under Proton or Wine; not tested there yet. Let me know if it works or not.)

QUICK CONTROLS
--------------
  F12              options menu (settings, controls, rules, About)
  F1               start a game (Declare War)
  Join screen      1 / 2 / 3 add a computer player as Blue / Red / Orange
  Gamepad          A fire, B/X/Y rotate, stick or D-pad move,
                   Back = F12 menu, Start = pause
Full key and pad layouts are on the Controls page in the F12 menu.


WHAT YOU NEED
-------------
The English DOS release of Rampart unprotected by HAL9000 / I.N.C (its
credits scroll thanks him). The mod patches the game's code at fixed
addresses, so only that exact release works. Rampart.exe checks every file
on startup and tells you which ones don't match.

Required files, with SHA-256 hashes: (You probably wont need to download these manually; just make sure they match if you have issues.)

  M1PPF.RSC    1ee5e1c717e4456f349bc15a45d8697bdcfe9e4c36503bae0642810fe2f77662
  M2PPF.RSC    d39b6adc3410f3c3349a94652d1946e0652d719804f56b09ebc309cbccd97a7f
  M3PPF.RSC    c29ec8f26c53f9592fa3a146f9375277e4830d77b3c2caa1dfe2f1e2ba571840
  MBATPF.GRA   59ff251a73a8ee1399b020f41fb1c8a40a99268639014299b92ca515abb2fb9b
  MHSCPF.GRA   6d63a4ce26426a66788476c49880bdb7e7d2d1fb744ba08f4f2af5e1999aec68
  MPAUSE.GRA   17f6aa6f2cef9a730040abc22ebaeb294592864e1650fef840f2ca355bf0739f
  MTETPF.GRA   bdb7a3dffd6b5b951cb656fa2c79ed1ce52a30289698f5e5a684a3ef8fee0f41
  MTIT2PF.GRA  e831c86b869bd3271be45bcae10946efd2110c395c13b65588c5f6e9020bc827
  MTITLE.RSC   2d09e2720bddbf2c6c382e8687ff6911c5e1842cafe20ac57c6f5e8b5918938d
  MTITPF.GRA   08196f522606722615ccb5ae99025b30dc47fc8ceab8785fa677fae51c0f20fc
  MTRANS.RSC   7f67f5f4972dd05bf4b100d21c44a8029f547ac48b1d274d59898ec023b341e3
  MTRNPF.GRA   042f4a794deae2872a120436e14257fa3bf7f3c04470f87bd55817abc7f0dd39
  MXPLPF.GRA   7a21d0a826bfc63ab7ab1c5497f2338c3acbef14773eb42b2c41b36f2927b6a8
  PAUSE.STR    aab927b4910da0ec95c109fd9c1325b9f4ec7c6d80ab3fe60f5bcd3d48c8f1b5
  RAMP.AD      b5e14a2577d19b8841b9fa176ed2f91b37bd81d2e11becf3d0295a6583712d4c
  RAMPART.EXE  05e14a4ed47d02b95608b17c6355f53db1fdb44c6d27619443c76c7b68eb2d17
  RMUSIC.RSC   238b71c463575474afbee36d17abe6076f2b881f286f11946aa68b9bca636157
  SOUND.RSC    54575e113b73e84bde130ca7942ce595e85a9450cca1422aabf228674064587b
  XLOGO2       5f6af4edc5e9ba32d56b68b4a77dc096ebacdaf359b0463066d85f9350aafc47

RAMPART.CFG and RAMPART.HIS are optional (they change as you play and save your scores and options, so they
aren't checked). Without them the mod uses default settings and starts a
fresh high-score table.

To check a file yourself, in PowerShell:
  Get-FileHash RAMPART.EXE
or in a Command Prompt:
  certutil -hashfile RAMPART.EXE SHA256


SETUP
-----
1. Unzip this package into a folder of its own.
2. Put your Rampart game folder (the one with RAMPART.EXE in it) inside that
   same folder. For example:

     Rampart Refortified\
       Rampart.exe
       README.txt
       RAMPART\            <- your game files
         RAMPART.EXE
         SOUND.RSC
         ...

   The mod looks next to Rampart.exe and up to two folders down, so a
   layout like Rampart_DOS_EN\RAMPART\ also works.
3. Run Rampart.exe.

If your game folder is somewhere else you must drag that folder (or its RAMPART.EXE) onto
Rampart.exe once. The mod remembers it after the first successful start.
You can also set the environment variable RAMPART_GAME to the folder.

Windows may warn that the program is unrecognized, since it isn't signed.
Click "More info", then "Run anyway". If you downloaded it off the Shifting Dollars GitHub, you wont get a virus.

Your files are never modified. Settings, records and high scores are saved
in %APPDATA%\Rampart. That folder also gets about.txt with the version
and the hashes of the game copy in use; include it when reporting a problem.


ELABORATED CONTROLS
--------------
  F12              options menu (settings, controls, rules, About.) Also has the option to restart or quit the game. Pressing F12 over shutdown options will do that, so be careful when closing the menu.
  F1               start a game (Declare War)
  Join screen      Pressing numbers 1 / 2 / 3 add a computer player as Blue / Red / Orange
  Gamepad          A fire, 
		   B/X/Y rotate, 
                   Stick or D-pad move, (Joystick registers one coordinate per move; must reset to deadzone)
                   Back = F12 menu  
		   Start = pause

Full key and pad layouts are on the Controls page in the F12 menu and can be rebound.


RUNNING THE PLAIN GAME IN DOSBOX OR DOSBOX-X
--------------------------------------------
The mod's features (bots, Endless, special cannons, gamepads and the rest)
live in Rampart.exe's own emulator. They are NOT available in DOSBox or
DOSBox-X. DOSBox runs the original, unmodified game, which is handy for
checking that your copy works or for playing it the way it shipped.

The copy is the same one you can play on Archive.org, which also runs on DOSBox.

I hate DOSBox so I pasted some instructions here:

DOSBox (0.74 or Staging) or DOSBox-X:

1. Start DOSBox / DOSBox-X.
2. Mount your game folder as drive C and start the game:

     mount c "C:\Games\Rampart Refortified\RAMPART"
     c:
     rampart

   (Use your real path. Quotes are needed if it has spaces.)

3. Sound: the original RAMPART.CFG is set to the PC speaker. Pick Sound
   Blaster in the game's sound setup screen, with port 220, IRQ 7 and
   DMA 1, which is DOSBox's default card. AdLib also works.
4. Speed: the default cycles=auto is fine. If the game runs too fast or too
   slow, Ctrl+F11 / Ctrl+F12 lower / raise the CPU speed (in DOSBox-X, use
   the CPU menu).
5. Fullscreen: Alt+Enter.

To skip typing the commands each time, add them to the [autoexec] section
at the end of your DOSBox config file:

     [autoexec]
     mount c "C:\Games\Rampart Refortified\RAMPART"
     c:
     rampart
     exit

DOSBox-X can also do this through its drive menu: Drive > C > Mount folder,
then type  c:  and  rampart  at the prompt.

Note: running the game in DOSBox rewrites RAMPART.CFG and RAMPART.HIS in
your game folder. This mod does not check those two files.


CREDITS
-------
Rampart: Atari Games (1990). DOS version: Bitmasters.
Unprotected release: HAL9000 / I.N.C.
Mod: Shifting Dollars.
