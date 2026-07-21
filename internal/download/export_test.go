package download

// SetMaxSizeForTest overrides d's artifact size cap for the duration of a
// test and returns a restore func. It exists so tests can exercise the
// under/at/over-limit boundary with small fixture bodies instead of an
// actual 2 GiB+ HTTP response.
func SetMaxSizeForTest(d *HTTPDownloader, n int64) (restore func()) {
	orig := d.maxSize
	d.maxSize = n
	return func() { d.maxSize = orig }
}
