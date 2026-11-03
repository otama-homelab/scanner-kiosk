//go:build linux && cgo && saneparams

// Command sane-parameters reads selected SANE parameters without acquisition.
// It is a separate native helper; the API/worker binary remains CGO-free.
package main

/*
#cgo LDFLAGS: -lsane
#include <sane/sane.h>
#include <stdlib.h>
#include <string.h>

static int find_option(SANE_Handle h, const char *name) {
 for (int i=1;i<1024;i++) {
  const SANE_Option_Descriptor *d=sane_get_option_descriptor(h,i);
  if (!d) break;
  if(d->name && strcmp(d->name,name)==0) return i;
 }
 return -1;
}
static SANE_Status check_string(SANE_Handle h,const char *name,const char *value) {
 int index=find_option(h,name);if(index<0) return SANE_STATUS_UNSUPPORTED;
 const SANE_Option_Descriptor *d=sane_get_option_descriptor(h,index);
 if(d->type!=SANE_TYPE_STRING || !SANE_OPTION_IS_ACTIVE(d->cap) || d->size<1 || d->size>4096) return SANE_STATUS_INVAL;
 char *buffer=calloc(1,d->size);if(!buffer) return SANE_STATUS_NO_MEM;
 SANE_Status status=sane_control_option(h,index,SANE_ACTION_GET_VALUE,buffer,NULL);
 if(status==SANE_STATUS_GOOD && (!memchr(buffer,0,d->size) || strcmp(buffer,value)!=0)) status=SANE_STATUS_INVAL;
 free(buffer);return status;
}
static SANE_Status set_string(SANE_Handle h,const char *name,const char *value) {
 int index=find_option(h,name);if(index<0) return SANE_STATUS_UNSUPPORTED;
 const SANE_Option_Descriptor *d=sane_get_option_descriptor(h,index);
 if(d->type!=SANE_TYPE_STRING || !SANE_OPTION_IS_ACTIVE(d->cap) || !SANE_OPTION_IS_SETTABLE(d->cap) || d->size<1 || d->size>4096 || strlen(value)>=(size_t)d->size) return SANE_STATUS_INVAL;
 char *buffer=calloc(1,d->size);if(!buffer) return SANE_STATUS_NO_MEM;
 strcpy(buffer,value);SANE_Int info=0;
 SANE_Status status=sane_control_option(h,index,SANE_ACTION_SET_VALUE,buffer,&info);
 free(buffer);
 if(status!=SANE_STATUS_GOOD || (info&SANE_INFO_INEXACT)) return SANE_STATUS_INVAL;
 return check_string(h,name,value);
}
static SANE_Status check_number(SANE_Handle h,const char *name,double value,SANE_Unit unit) {
 int index=find_option(h,name);if(index<0) return SANE_STATUS_UNSUPPORTED;
 const SANE_Option_Descriptor *d=sane_get_option_descriptor(h,index);
 if(!SANE_OPTION_IS_ACTIVE(d->cap) || d->unit!=unit || d->size!=sizeof(SANE_Word)) return SANE_STATUS_INVAL;
 SANE_Word wanted;
 if(d->type==SANE_TYPE_FIXED) wanted=SANE_FIX(value);
 else if(d->type==SANE_TYPE_INT && unit==SANE_UNIT_DPI) wanted=(SANE_Word)value;
 else return SANE_STATUS_INVAL;
 SANE_Word actual=0;
 SANE_Status status=sane_control_option(h,index,SANE_ACTION_GET_VALUE,&actual,NULL);
 return status==SANE_STATUS_GOOD && actual==wanted ? SANE_STATUS_GOOD : SANE_STATUS_INVAL;
}
static SANE_Status set_number(SANE_Handle h,const char *name,double value,SANE_Unit unit) {
 int index=find_option(h,name);if(index<0) return SANE_STATUS_UNSUPPORTED;
 const SANE_Option_Descriptor *d=sane_get_option_descriptor(h,index);
 if(!SANE_OPTION_IS_ACTIVE(d->cap) || !SANE_OPTION_IS_SETTABLE(d->cap) || d->unit!=unit || d->size!=sizeof(SANE_Word)) return SANE_STATUS_INVAL;
 SANE_Word wanted;
 if(d->type==SANE_TYPE_FIXED) wanted=SANE_FIX(value);
 else if(d->type==SANE_TYPE_INT && unit==SANE_UNIT_DPI) wanted=(SANE_Word)value;
 else return SANE_STATUS_INVAL;
 SANE_Word actual=wanted;SANE_Int info=0;
 SANE_Status status=sane_control_option(h,index,SANE_ACTION_SET_VALUE,&actual,&info);
 if(status!=SANE_STATUS_GOOD || (info&SANE_INFO_INEXACT) || actual!=wanted) return SANE_STATUS_INVAL;
 return check_number(h,name,value,unit);
}
static SANE_Status configure(SANE_Handle h,const char *mode,int dpi,double width,double height) {
 if(set_string(h,"source","Flatbed")!=SANE_STATUS_GOOD || set_string(h,"mode",mode)!=SANE_STATUS_GOOD || set_number(h,"resolution",dpi,SANE_UNIT_DPI)!=SANE_STATUS_GOOD || set_number(h,"tl-x",0,SANE_UNIT_MM)!=SANE_STATUS_GOOD || set_number(h,"tl-y",0,SANE_UNIT_MM)!=SANE_STATUS_GOOD || set_number(h,"br-x",width,SANE_UNIT_MM)!=SANE_STATUS_GOOD || set_number(h,"br-y",height,SANE_UNIT_MM)!=SANE_STATUS_GOOD) return SANE_STATUS_INVAL;
 // Later option changes can invalidate earlier values. Re-read the final set.
 if(check_string(h,"source","Flatbed")!=SANE_STATUS_GOOD || check_string(h,"mode",mode)!=SANE_STATUS_GOOD || check_number(h,"resolution",dpi,SANE_UNIT_DPI)!=SANE_STATUS_GOOD || check_number(h,"tl-x",0,SANE_UNIT_MM)!=SANE_STATUS_GOOD || check_number(h,"tl-y",0,SANE_UNIT_MM)!=SANE_STATUS_GOOD || check_number(h,"br-x",width,SANE_UNIT_MM)!=SANE_STATUS_GOOD || check_number(h,"br-y",height,SANE_UNIT_MM)!=SANE_STATUS_GOOD) return SANE_STATUS_INVAL;
 return SANE_STATUS_GOOD;
}
*/
import "C"

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"scanner-kiosk/internal/scanner/sane"
)

func inspect() (sane.Parameters, error) {
	var out sane.Parameters
	device := flag.String("device-name", "", "")
	size := flag.String("size", "", "")
	color := flag.String("color", "", "")
	dpi := flag.Int("dpi", 0, "")
	allowTest := flag.Bool("allow-test-backend", false, "")
	flag.Parse()
	fail := errors.New("scanner parameters unavailable")
	selection := sane.Selection{Size: *size, Color: *color, DPI: *dpi}
	width, height, err := selection.AreaMM()
	backend := "airscan"
	if *allowTest && *device == "test:0" {
		backend = "test"
	} else if !strings.HasPrefix(*device, "airscan:") || len(*device) > 4096 || strings.ContainsAny(*device, "\x00\r\n") {
		return out, fail
	}
	if err != nil || flag.NArg() != 0 {
		return out, fail
	}
	root := os.Getenv("SANE_CONFIG_DIR")
	info, err := os.Lstat(root)
	if err != nil || !filepath.IsAbs(root) || filepath.Clean(root) != root || strings.Contains(root, ":") || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return out, fail
	}
	file, err := os.Open(filepath.Join(root, "dll.conf"))
	if err != nil {
		return out, fail
	}
	data, err := io.ReadAll(io.LimitReader(file, 64))
	closeErr := file.Close()
	if err != nil || closeErr != nil || string(data) != backend+"\n" {
		return out, fail
	}
	var version C.SANE_Int
	if C.sane_init(&version, nil) != C.SANE_STATUS_GOOD {
		return out, fail
	}
	defer C.sane_exit()
	name := C.CString(*device)
	defer C.free(unsafe.Pointer(name))
	var handle C.SANE_Handle
	if C.sane_open(name, &handle) != C.SANE_STATUS_GOOD {
		return out, fail
	}
	defer C.sane_close(handle)
	mode := "Color"
	if *color == "gray" {
		mode = "Gray"
	}
	nativeMode := C.CString(mode)
	defer C.free(unsafe.Pointer(nativeMode))
	if C.configure(handle, nativeMode, C.int(*dpi), C.double(width), C.double(height)) != C.SANE_STATUS_GOOD {
		return out, fail
	}
	var params C.SANE_Parameters
	if C.sane_get_parameters(handle, &params) != C.SANE_STATUS_GOOD {
		return out, fail
	}
	channels := 0
	switch params.format {
	case C.SANE_FRAME_GRAY:
		channels = 1
	case C.SANE_FRAME_RGB:
		channels = 3
	}
	expectedChannels := 1
	if *color == "color" {
		expectedChannels = 3
	}
	if params.depth != 8 || channels != expectedChannels || params.last_frame != C.SANE_TRUE || params.pixels_per_line < 1 || params.lines < 1 || int64(params.bytes_per_line) < int64(params.pixels_per_line)*int64(channels) || int64(version)>>24 != 1 {
		return out, fail
	}
	out = sane.Parameters{ProtocolVersion: 1, Size: *size, Color: *color, DPI: *dpi, Width: int(params.pixels_per_line), Height: int(params.lines), Depth: int(params.depth), Channels: channels, BytesPerLine: int(params.bytes_per_line), LastFrame: true, SANEVersion: int64(version)}
	return out, nil
}
func main() {
	params, err := inspect()
	if err != nil {
		fmt.Fprintln(os.Stderr, "scanner parameters unavailable")
		os.Exit(1)
	}
	if json.NewEncoder(os.Stdout).Encode(params) != nil {
		os.Exit(1)
	}
}
