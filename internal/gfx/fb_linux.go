package gfx

import (
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"os"
	"sync"

	"golang.org/x/sys/unix"
)

const (
	fbioGetVScreenInfo = 0x4600
	fbioGetFScreenInfo = 0x4602
	kdSetMode          = 0x4B3A
	kdText             = 0
	kdGraphics         = 1
)

type channel struct{ off, length uint32 }

// Display is the machine's screen, drawn with graphics.
type Display struct {
	mu          sync.Mutex
	emu         *Emu
	mem         []byte
	f           *os.File
	tty         *os.File
	w, h        int
	stride, bpp int
	r, g, b     channel
	scale       int
}

func ioctl(fd uintptr, req uintptr, p []byte) error {
	_, _, e := unix.Syscall(unix.SYS_IOCTL, fd, req, uintptr(unsafePointer(p)))
	if e != 0 {
		return e
	}
	return nil
}

// ioctlVal is for requests whose argument is a number, not a pointer.
func ioctlVal(fd, req, val uintptr) {
	unix.Syscall(unix.SYS_IOCTL, fd, req, val)
}

// OpenDisplay takes over the frame buffer. It fails, and leaves the console alone, when there is no
// usable one; callers then fall back to ordinary text.
func OpenDisplay() (*Display, error) {
	f, err := os.OpenFile("/dev/fb0", os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	v := make([]byte, 160)
	fx := make([]byte, 80)
	if err := ioctl(f.Fd(), fbioGetVScreenInfo, v); err != nil {
		f.Close()
		return nil, err
	}
	if err := ioctl(f.Fd(), fbioGetFScreenInfo, fx); err != nil {
		f.Close()
		return nil, err
	}
	u := func(i int) uint32 { return binary.LittleEndian.Uint32(v[i*4:]) }
	d := &Display{f: f, w: int(u(0)), h: int(u(1)), bpp: int(u(6))}
	d.r, d.g, d.b = channel{u(8), u(9)}, channel{u(11), u(12)}, channel{u(14), u(15)}
	d.stride = int(binary.LittleEndian.Uint32(fx[48:]))
	if d.stride == 0 {
		d.stride = int(u(2)) * d.bpp / 8
	}
	if (d.bpp != 32 && d.bpp != 16) || d.w < 640 || d.h < 400 || d.r.length == 0 {
		f.Close()
		return nil, fmt.Errorf("unsupported frame buffer %dx%d at %d bits", d.w, d.h, d.bpp)
	}
	d.mem, err = unix.Mmap(int(f.Fd()), 0, d.stride*int(u(3)), unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		f.Close()
		return nil, err
	}
	d.scale = ScaleFor(d.w)
	cw, ch := Metrics(d.scale)
	d.emu = NewEmu(d.w/cw, d.h/ch)
	// A text console would draw its cursor and its own text over ours.
	if t, err := os.OpenFile("/dev/tty1", os.O_RDWR, 0); err == nil {
		ioctlVal(t.Fd(), kdSetMode, kdGraphics)
		d.tty = t
	}
	return d, nil
}

// Size is the screen in characters.
func (d *Display) Size() (cols, rows int) { return d.emu.Cols, d.emu.Rows }

// Write feeds output to the screen and redraws it.
func (d *Display) Write(p []byte) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.mem == nil {
		return 0, errors.New("display closed")
	}
	d.emu.Write(p)
	d.blit(d.emu.Render(d.w, d.h, d.scale))
	return len(p), nil
}

func (d *Display) blit(img *image.RGBA) {
	pack := func(r, g, b uint8) uint32 {
		return uint32(r)>>(8-d.r.length)<<d.r.off | uint32(g)>>(8-d.g.length)<<d.g.off | uint32(b)>>(8-d.b.length)<<d.b.off
	}
	bytesPer := d.bpp / 8
	for y := 0; y < d.h; y++ {
		row := d.mem[y*d.stride : y*d.stride+d.w*bytesPer]
		src := img.Pix[y*img.Stride : y*img.Stride+d.w*4]
		for x := 0; x < d.w; x++ {
			v := pack(src[x*4], src[x*4+1], src[x*4+2])
			if bytesPer == 4 {
				binary.LittleEndian.PutUint32(row[x*4:], v)
			} else {
				binary.LittleEndian.PutUint16(row[x*2:], uint16(v))
			}
		}
	}
}

// Close gives the console back.
func (d *Display) Close() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.tty != nil {
		ioctlVal(d.tty.Fd(), kdSetMode, kdText)
		d.tty.Close()
	}
	if d.mem != nil {
		unix.Munmap(d.mem)
		d.mem = nil
	}
	d.f.Close()
}
