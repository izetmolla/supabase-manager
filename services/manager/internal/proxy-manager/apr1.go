package proxymanager

import (
	"crypto/md5"
	"crypto/rand"
	"strings"
)

const apr1Alphabet = "./0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// apr1Hash returns an Apache MD5 ($apr1$) password hash. It is the strongest scheme that both
// nginx on Alpine (musl crypt has no bcrypt) and Traefik verify.
func apr1Hash(password string) string {
	salt := make([]byte, 8)
	buf := make([]byte, 8)
	_, _ = rand.Read(buf)
	for i := range salt {
		salt[i] = apr1Alphabet[int(buf[i])%len(apr1Alphabet)]
	}
	return apr1WithSalt(password, string(salt))
}

func apr1WithSalt(password, salt string) string {
	const magic = "$apr1$"
	pw := []byte(password)
	sl := []byte(salt)

	alt := md5.Sum(append(append(append([]byte{}, pw...), sl...), pw...))

	ctx := md5.New()
	ctx.Write(pw)
	ctx.Write([]byte(magic))
	ctx.Write(sl)
	for i := len(pw); i > 0; i -= 16 {
		ctx.Write(alt[:min(i, 16)])
	}
	for i := len(pw); i > 0; i >>= 1 {
		if i&1 == 1 {
			ctx.Write([]byte{0})
		} else {
			ctx.Write(pw[:1])
		}
	}
	final := ctx.Sum(nil)

	for i := range 1000 {
		c := md5.New()
		if i&1 == 1 {
			c.Write(pw)
		} else {
			c.Write(final)
		}
		if i%3 != 0 {
			c.Write(sl)
		}
		if i%7 != 0 {
			c.Write(pw)
		}
		if i&1 == 1 {
			c.Write(final)
		} else {
			c.Write(pw)
		}
		final = c.Sum(nil)
	}

	var out strings.Builder
	enc := func(a, b, c byte, n int) {
		v := uint(a)<<16 | uint(b)<<8 | uint(c)
		for range n {
			out.WriteByte(apr1Alphabet[v&0x3f])
			v >>= 6
		}
	}
	enc(final[0], final[6], final[12], 4)
	enc(final[1], final[7], final[13], 4)
	enc(final[2], final[8], final[14], 4)
	enc(final[3], final[9], final[15], 4)
	enc(final[4], final[10], final[5], 4)
	enc(0, 0, final[11], 2)
	return magic + salt + "$" + out.String()
}
