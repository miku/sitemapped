// sitemapped turns a single URL pointing to a sitemap into a list of URL,
// resolving sitemapindex type sitemaps, too.
//
// $ sitemapped https://core.ac.uk/sitemap.xml
//
// Some sitemap index style sitemaps may point to thousands of actual sitemaps.
package main

import (
	"bufio"
	"bytes"
	"crypto/sha1"
	"encoding/xml"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path"

	"github.com/adrg/xdg"
	"github.com/schollz/progressbar/v3"
	"github.com/sethgrid/pester"
)

// SitemapIndexEntry is an entry in a sitemap index style sitemap.
type SitemapIndexEntry struct {
	XMLName xml.Name `xml:"sitemap"`
	Text    string   `xml:",chardata"`
	Loc     string   `xml:"loc"`     // https://core.ac.uk/sitema...
	Lastmod string   `xml:"lastmod"` // 2021-01-08, 2021-01-08, 2...
}

// Sitemapindex was generated 2024-07-01 15:50:15 by tir on reka with zek 0.1.24.
type Sitemapindex struct {
	XMLName xml.Name            `xml:"sitemapindex"`
	Text    string              `xml:",chardata"`
	Xmlns   string              `xml:"xmlns,attr"`
	Sitemap []SitemapIndexEntry `xml:"sitemap"`
}

// Urlset was generated 2024-07-01 20:25:25 by tir on reka with zek 0.1.24.
type Urlset struct {
	XMLName xml.Name `xml:"urlset"`
	Text    string   `xml:",chardata"`
	Xmlns   string   `xml:"xmlns,attr"`
	URL     []struct {
		Text string `xml:",chardata"`
		Loc  string `xml:"loc"` // https://core.ac.uk/displa...
	} `xml:"url"`
}

var (
	defaultCachePath = path.Join(xdg.CacheHome, "sitemap")
	httpClient       = pester.New()

	maxRetries = flag.Int("r", 3, "max HTTP client retries")
	cacheDir   = flag.String("cache-dir", defaultCachePath, "path to cache directory")
	force      = flag.Bool("f", false, "force redownload, even if cached file exists")
)

func main() {
	flag.Parse()
	if flag.NArg() == 0 {
		log.Fatal("a sitemap.xml URL is required")
	}
	if err := os.MkdirAll(*cacheDir, 755); err != nil {
		log.Fatal(err)
	}
	cache := &Cache{Dir: *cacheDir}
	httpClient.MaxRetries = *maxRetries
	httpClient.Backoff = pester.ExponentialBackoff
	httpClient.RetryOnHTTP429 = true
	sitemapURL := flag.Arg(0) // sitemap or sitemapindex
	fn, err := cache.URL(sitemapURL, nil)
	if err != nil {
		log.Fatal(err)
	}
	isIndex, err := isSitemapIndex(fn)
	if err != nil {
		log.Fatal(err)
	}
	f, err := os.Open(fn)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	bw := bufio.NewWriter(os.Stdout)
	defer bw.Flush()
	if isIndex {
		err = urlsFromSitemapIndex(cache, f, bw)
	} else {
		err = urlsFromSitemap(f, bw)
	}
	if err != nil {
		log.Fatal(err)
	}
}

// isSitemapIndex returns true if this an index.
func isSitemapIndex(filename string) (bool, error) {
	f, err := os.Open(filename)
	if err != nil {
		return false, err
	}
	defer f.Close()
	buf := make([]byte, 1024)
	_, err = f.Read(buf)
	if err != nil {
		return false, err
	}
	return bytes.Contains(buf, []byte("sitemapindex")), nil
}

func urlsFromSitemapIndex(cache *Cache, r io.Reader, w io.Writer) error {
	dec := xml.NewDecoder(r)
	var smi Sitemapindex
	err := dec.Decode(&smi)
	if err != nil {
		return err
	}
	for _, sm := range smi.Sitemap {
		fn, err := cache.URL(sm.Loc, &DownloadOpts{Force: *force})
		if err != nil {
			return err
		}
		f, err := os.Open(fn)
		if err != nil {
			return err
		}
		dec = xml.NewDecoder(f)
		var uset Urlset
		if err := dec.Decode(&uset); err != nil {
			log.Fatal(err)
		}
		for _, u := range uset.URL {
			_, err := fmt.Fprintln(w, u.Loc)
			if err != nil {
				return err
			}
		}
		if err := f.Close(); err != nil {
			return err
		}
	}
	log.Printf("found locs: %d", len(smi.Sitemap))
	return nil
}

func urlsFromSitemap(r io.Reader, w io.Writer) error {
	dec := xml.NewDecoder(r)
	var urlset Urlset
	err := dec.Decode(&urlset)
	if err != nil {
		return err
	}
	for _, u := range urlset.URL {
		_, err := fmt.Fprintln(w, u.Loc)
		if err != nil {
			return err
		}
	}
	return nil
}

type Cache struct {
	Dir string
}

type DownloadOpts struct {
	Filename string // a specific filename to use, if any
	Force    bool   // attempt redownload in any case
}

// URL returns the path to cached file for a given URL. If force is true,
// redownload, even if copy exists.
func (c *Cache) URL(url string, opts *DownloadOpts) (string, error) {
	dir := c.Dir
	if opts == nil || opts.Filename == "" {
		h := sha1.New()
		_, _ = h.Write([]byte(url))
		digest := fmt.Sprintf("%x", h.Sum(nil))
		shard := digest[:2]
		opts = &DownloadOpts{Filename: digest}
		dir = path.Join(c.Dir, shard)
	}
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return "", err
		}
	}
	dst := path.Join(dir, opts.Filename)
	if _, err := os.Stat(dst); os.IsNotExist(err) || opts.Force {
		if err := DownloadFile(url, dst); err != nil {
			return "", err
		}
	}
	return dst, nil
}

// DownloadFile retrieves a file from URL, atomically.
func DownloadFile(url string, dst string) error {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	// TODO: use temporary file
	tmpf := dst + ".wip"
	f, err := os.OpenFile(tmpf, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	bar := progressbar.DefaultBytes(
		resp.ContentLength,
		"downloading",
	)
	_, err = io.Copy(io.MultiWriter(f, bar), resp.Body)
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmpf, dst)
}