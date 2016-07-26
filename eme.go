// EME (ECB-Mix-ECB) is a wide-block encryption mode presented in the 2003 paper
// "A Parallelizable Enciphering Mode" by Halevi and Rogaway.
// This is an implementation of EME in Go.
package eme

import (
	"crypto/cipher"
	"log"
)

const (
	DirectionEncrypt = true
	DirectionDecrypt = false
)

// multByTwo - GF multiplication as specified in the EME-32 draft
func multByTwo(out []byte, in []byte) {
	if len(in) != 16 {
		panic("len must be 16")
	}
	tmp := make([]byte, 16)

	tmp[0] = 2 * in[0]
	if in[15] >= 128 {
		tmp[0] = tmp[0] ^ 135
	}
	for j := 1; j < 16; j++ {
		tmp[j] = 2 * in[j]
		if in[j-1] >= 128 {
			tmp[j] += 1
		}
	}
	copy(out, tmp)
}

func xorBlocks(out []byte, in1 []byte, in2 []byte) {
	if len(in1) != len(in2) {
		log.Panicf("len(in1)=%d is not equal to len(in2)=%d", len(in1), len(in2))
	}

	for i := range in1 {
		out[i] = in1[i] ^ in2[i]
	}
}

// aesTransform - encrypt or decrypt (according to "direction") using block
// cipher "bc" (typically AES)
func aesTransform(dst []byte, src []byte, direction bool, bc cipher.Block) {
	if direction == DirectionEncrypt {
		bc.Encrypt(dst, src)
		return
	} else if direction == DirectionDecrypt {
		bc.Decrypt(dst, src)
		return
	} else {
		log.Panicf("unknown direction %d", direction)
	}
}

// tabulateL - calculate L_i for messages up to a length of m cipher blocks
func tabulateL(bc cipher.Block, m int) [][]byte {
	/* set L0 = 2*AESenc(K; 0) */
	eZero := make([]byte, 16)
	Li := make([]byte, 16)
	bc.Encrypt(Li, eZero)

	LTable := make([][]byte, m)
	// Allocate pool once and slice into m pieces in the loop
	pool := make([]byte, m*16)
	for i := 0; i < m; i++ {
		multByTwo(Li, Li)
		LTable[i] = pool[i*16 : (i+1)*16]
		copy(LTable[i], Li)
	}
	return LTable
}

type lCacheContainer struct {
	LTable  [][]byte
	enabled bool
}

// precompute LTable for maximum length
// Note that LTable depends on the AES key, so you must run precompute or clear
// when the key changes.
func (lc *lCacheContainer) precompute(bc cipher.Block) {
	lc.LTable = tabulateL(bc, 16*8) // 16*8 = maximum length
	lc.enabled = true
}

// clear LTable cache
func (lc *lCacheContainer) clear() {
	lc.enabled = false
}

var lTableCache lCacheContainer

// Transform - EME-encrypt or EME-decrypt, according to "direction"
// (defined in the constants directionEncrypt and directionDecrypt).
// The data in "P" is en- or decrypted with the block ciper "bc" under tweak "T".
// The result is returned in a freshly allocated slice.
func Transform(bc cipher.Block, T []byte, P []byte, direction bool) (C []byte) {
	if bc.BlockSize() != 16 {
		log.Panicf("Using a block size other than 16 is not implemented")
	}
	if len(T) != 16 {
		log.Panicf("Tweak must be 16 bytes long, is %d", len(T))
	}
	if len(P)%16 != 0 {
		log.Panicf("Data length %d is not a multiple of 16", len(P))
	}
	m := len(P) / 16
	if m == 0 || m > 16*8 {
		log.Panicf("EME operates on 1-%d block-cipher blocks", 16*8)
	}

	C = make([]byte, len(P))

	var LTable [][]byte
	if lTableCache.enabled {
		LTable = lTableCache.LTable
	} else {
		LTable = tabulateL(bc, m)
	}

	PPj := make([]byte, 16)
	for j := 0; j < m; j++ {
		Pj := P[j*16 : (j+1)*16]
		/* PPj = 2**(j-1)*L xor Pj */
		xorBlocks(PPj, Pj, LTable[j])
		/* PPPj = AESenc(K; PPj) */
		aesTransform(C[j*16:(j+1)*16], PPj, direction, bc)
	}

	/* MP =(xorSum PPPj) xor T */
	MP := make([]byte, 16)
	xorBlocks(MP, C[0:16], T)
	for j := 1; j < m; j++ {
		xorBlocks(MP, MP, C[j*16:(j+1)*16])
	}

	/* MC = AESenc(K; MP) */
	MC := make([]byte, 16)
	aesTransform(MC, MP, direction, bc)

	/* M = MP xor MC */
	M := make([]byte, 16)
	xorBlocks(M, MP, MC)
	CCCj := make([]byte, 16)
	for j := 1; j < m; j++ {
		multByTwo(M, M)
		/* CCCj = 2**(j-1)*M xor PPPj */
		xorBlocks(CCCj, C[j*16:(j+1)*16], M)
		copy(C[j*16:(j+1)*16], CCCj)
	}

	/* CCC1 = (xorSum CCCj) xor T xor MC */
	CCC1 := make([]byte, 16)
	xorBlocks(CCC1, MC, T)
	for j := 1; j < m; j++ {
		xorBlocks(CCC1, CCC1, C[j*16:(j+1)*16])
	}
	copy(C[0:16], CCC1)

	for j := 0; j < m; j++ {
		/* CCj = AES-enc(K; CCCj) */
		aesTransform(C[j*16:(j+1)*16], C[j*16:(j+1)*16], direction, bc)
		/* Cj = 2**(j-1)*L xor CCj */
		xorBlocks(C[j*16:(j+1)*16], C[j*16:(j+1)*16], LTable[j])
	}

	return C
}
