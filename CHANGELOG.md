# 1.1 (18 July 2023)

New features:

- An `-i` option for the list command, which filters for already-installed mods
- A download progress meter for mods that take longer than about a second to fetch
- The MODLINKSURL environment variable can be set to fetch the modlinks file from an alternative
  URL
- Mods that use platform-specific links - such as Pale Court - can now be downloaded

Bug fixes:

- When using the publish command, the expected order of the manifest fields is now preserved