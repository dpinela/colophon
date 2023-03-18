# Colophon

This is a command-line mod installer for Hollow Knight. It reads the list
of mods from the same place as [Scarab][] and can therefore access all the
same mods.

[Scarab]: https://github.com/fifty-six/Scarab

## Setup

To install this tool, grab a binary release, or if you have a [Go][] toolchain installed, run this command to compile it from source:

    $ go install github.com/dpinela/colophon/cmd/hkmod@latest

Then, set the HK15PATH environment variable to the path to your Hollow Knight 
installation (the directory containing the Assembly-CSharp.dll file). The game must 
already have the [Modding API][] installed; this tool does not yet have a way of 
doing that for you.

[Modding API]: https://github.com/hk-modding/api
[Go]: https://go.dev

## Commands

### list

The list command serves to look up information about any mod listed on [modlinks][].
In its simplest form, it prints a list of all available mods:

    $ hkmod list
    Abskoth
    AbsoluteZote
    Additional Challenge
    Additional Timelines
    AdditionalMaps
    Always Furious
    Archipelago
    Archipelago Map Mod
    AsciiCamera
    Aspid Queen
    Aspidnest
    AspyCompanion
    ...

Using the `-s` option, you can reduce the list to mods containing a particular string
(case-insensitive) in their names:

    $ hkmod list -s rando
    BenchRando
    Breakable Wall Randomizer
    Curse Randomizer
    DarknessRandomizer
    Lore Randomizer
    Rando Vanilla Tracker
    RandoChecksCounter
    RandoMapMod
    RandoPlus
    RandoSettingsManager
    ...

The `-d` option adds more detailed information about each mod:

    $ hkmod list -d -s levers
    Randomizable Levers
        Version: 1.2.4.0
        Repository: https://github.com/flibber-hk/HollowKnight.RandomizableLevers/
        Dependencies: ItemChanger
        Randomizer 4 addon that adds the option to randomize levers. Activate lever rando in the Connections menu of Randomizer 4.

`-d` can technically be used without `-s` as well, but there is usually little reason
to do that.

[modlinks]: https://github.com/hk-modding/modlinks

### install

The install command downloads and installs one or more mods that are listed on
modlinks, along with any necessary dependencies. It takes as arguments the list of
mods to install:

    $ hkmod install levers reopencity randoplus darknessrando randomapmod moredoors itemsync randosettingsmanager scatternest journalrando

The arguments usually do not need to match mod names exactly; for each one, the mod
to install is selected by the first of the following that matches one and only one mod:

- A partial case-insensitive match
- A full case-insensitive match
- A full case-sensitive match

If hkmod can't disambiguate which mod you want, it will print an error message
explaining why, and skip installing that mod:

    $ hkmod install rando
    "rando" is ambiguous: matches Breakable Wall Randomizer, RandoZoomZoom, Random Pantheons, RandoPlus, Randomizable Levers, Rope Rando, TrandoPlus, RandomizerSettingsRandomizer, Randomizer 4, RandomizerCore, BenchRando, RandomGravityChange, RandomTeleport, Toggle Rando Split Options, RandoStats, RandoChecksCounter, TheRealJournalRando, RandoSettingsManager, RandomCompanions, RandomCharm, Rando Vanilla Tracker, Lore Randomizer, DarknessRandomizer, RandoMapMod, Curse Randomizer

    $ hkmod install modthatdoesnotexistatallandneverwill
    "modthatdoesnotexistatallandneverwill" matches no mods

Once it resolves which mods to get, hkmod installs the latest available version of
each of them, **irrespective of which, if any, version you had installed before.**
It makes no attempt to keep track of which mod versions are currently installed in
any way; to save time and bandwidth, it instead caches downloads and relies on the
hash listed in modlinks to check whether the cached files are still valid and
up-to-date.

For most mods, installing a new version **entirely removes** the previously installed
one, so any custom files added to that mod's folder will be deleted as well. An
exception is made for Custom Knight, so that you can update that mod while keeping
any skins you've installed.

### installfile

The installfile command installs a mod from a manually-specified file or URL. It
can install any mod whether or not it exists on modlinks, and does not perform any
checksum verification. Unlike the install command, the mod name must be given
exactly.

One use of this command is to install older or newer versions of mods than the ones
on modlinks: for example

    $ hkmod install Transcendence https://github.com/dpinela/Transcendence/releases/download/v1.3.5/Transcendence.zip

installs an older version of Transcendence.

### yeet

The yeet command fully removes the named mods. It uses the same matching algorithm
as the install command, so partial matches will work when unambiguous:

    $ hkmod yeet levers
    Yeeted Randomizable Levers

*Unlike* the install command, there is no special treatment for Custom Knight;
uninstalling that mod will also uninstall all of your skins. Also, this command can 
target any mod you have installed, regardless of source, including mods that do not
exist on modlinks or were installed by a different tool.

### publish

The publish command is a small convenience for mod developers. It automatically
modifies a local copy of the modlinks file to include a new mod, or an updated version
of an existing one. In its simplest form, all that is required is the URL of the
released mod file:

    $ hkmod publish -url https://github.com/dpinela/Transcendence/releases/download/v1.4.1/Transcendence.zip

In this form, it looks for the ModLinks.xml file in the current directory, and it 
attempts to derive the name and version of the mod from the URL, and
keeps the existing description, repository link, and dependencies (if any). Additional
arguments, `-deps`, `-desc`, `-name`, `-repo` and `-version` exist for specifying
those things if necessary, and `-modlinks` to specify where to find ModLinks.xml.

## Where does the name come from?

[Colophon][] is the name of a rare kind of [stag][] beetle, which are in turn closely
related to [scarabs][].

[Colophon]: https://en.wikipedia.org/wiki/Colophon_(beetle)
[stag]: https://www.speedrun.com/hkmemes?h=All_Stag_Stations-1.4.3.2_NMG&x=vdo18012-5lypejyl.zqo0vvxq
[scarabs]: https://en.wikipedia.org/wiki/Scarabaeidae