# sitemapped

> Turn a sitemap URL into a list of URLs.

This tool will work with both sitemaps and sitemap indices.

Sitemaps are cached locally, following the [XDG
standard](https://wiki.archlinux.org/title/XDG_Base_Directory); this speeds up
subsequent invocations, but it is also possible to force a redownload.

Sitemap protocol spec:
[www.sitemaps.org/protocol.html](https://www.sitemaps.org/protocol.html). Note:
we do not support feeds and plain text sitemaps - maybe just use
[curl](https://curl.se/) for that?

## Install

Install latest version w/ Go toolchain.

```
$ go install github.com/miku/sitemapped@latest
```

## Usage

```
$ sitemapped
Usage of sitemapped:
  -cache-dir string
        path to cache directory (default "/home/tir/.cache/sitemap")
  -f    force redownload, even if cached file exists
  -r int
        max HTTP client retries (default 3)
```

## Examples

```
$ sitemapped https://besra.net/sitemap.xml | head
https://besra.net/
https://besra.net/index.php/upcoming-conferences/icbsss/
https://besra.net/index.php/upcoming-conferences/
https://besra.net/index.php/icbmge/
https://besra.net/index.php/past-conferences/
https://besra.net/index.php/publications/
https://besra.net/index.php/payment/
https://besra.net/index.php/home/
https://besra.net/index.php/advisory-board/
https://besra.net/index.php/ijabs/
```

Sitemap index example:

```
$ sitemapped https://core.ac.uk/sitemap.xml
https://core.ac.uk/reader/14671
https://core.ac.uk/download/pdf/14671.pdf
https://core.ac.uk/display/14671
https://core.ac.uk/reader/14696
https://core.ac.uk/download/pdf/14696.pdf
https://core.ac.uk/display/14696
https://core.ac.uk/reader/14705
https://core.ac.uk/download/pdf/14705.pdf
https://core.ac.uk/display/14705
https://core.ac.uk/reader/14759
```
