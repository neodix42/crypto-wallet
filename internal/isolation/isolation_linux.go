// Package isolation installs a process-wide seccomp filter before secret input.
package isolation

/*
#cgo LDFLAGS: -l:libseccomp.so.2
#include <stdint.h>
#include <sys/prctl.h>
#include <sys/resource.h>
#include <sys/socket.h>
#include <errno.h>
typedef void* scmp_filter_ctx;
struct scmp_arg_cmp {unsigned int arg;int op;uint64_t datum_a;uint64_t datum_b;};
extern scmp_filter_ctx seccomp_init(uint32_t);
extern int seccomp_attr_set(scmp_filter_ctx,int,uint32_t);
extern int seccomp_rule_add(scmp_filter_ctx,uint32_t,int,unsigned int,...);
extern int seccomp_syscall_resolve_name(const char*);
extern int seccomp_load(scmp_filter_ctx);
extern void seccomp_release(scmp_filter_ctx);
static int isolate(){
 struct rlimit r={0,0};if(setrlimit(RLIMIT_CORE,&r)||prctl(PR_SET_DUMPABLE,0)||prctl(PR_SET_NO_NEW_PRIVS,1,0,0,0))return -1;
 scmp_filter_ctx c=seccomp_init(0x7fff0000);if(!c)return -1;
 if(seccomp_attr_set(c,4,1)){seccomp_release(c);return -1;}
 int domains[]={AF_INET,AF_INET6,AF_PACKET};int calls[]={seccomp_syscall_resolve_name("socket"),seccomp_syscall_resolve_name("socketpair")};
 for(int i=0;i<2;i++)for(int j=0;j<3;j++){struct scmp_arg_cmp a={0,4,domains[j],0};if(seccomp_rule_add(c,0x00050000|EPERM,calls[i],1,a)){seccomp_release(c);return -1;}}
 const char*blocked[]={"io_uring_setup","io_uring_enter","io_uring_register"};for(int i=0;i<3;i++){int n=seccomp_syscall_resolve_name(blocked[i]);if(n>=0&&seccomp_rule_add(c,0x00050000|EPERM,n,0)){seccomp_release(c);return -1;}}
 int result=seccomp_load(c);seccomp_release(c);return result;
}
static int dump_disabled(){struct rlimit r;if(getrlimit(RLIMIT_CORE,&r))return 0;return r.rlim_cur==0&&prctl(PR_GET_DUMPABLE)==0;}
*/
import "C"
import (
	"errors"
	"syscall"
)

func Install() error {
	if C.isolate() != 0 || C.dump_disabled() == 0 {
		return errors.New("signer isolation unavailable")
	}
	for _, af := range []int{syscall.AF_INET, syscall.AF_INET6} {
		for _, kind := range []int{syscall.SOCK_STREAM, syscall.SOCK_DGRAM} {
			fd, e := syscall.Socket(af, kind, 0)
			if e == nil {
				syscall.Close(fd)
				return errors.New("signer network self-check failed")
			}
		}
	}
	return nil
}
