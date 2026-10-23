"""Test-only X11 input against a private Xvfb display. No clipboard or secret
arguments/environment. Public synthetic credentials belong only in test code."""
import ctypes as C,time,tempfile
from PIL import Image
X=C.CDLL('libX11.so.6');T=C.CDLL('libXtst.so.6')
X.XOpenDisplay.argtypes=[C.c_char_p];X.XOpenDisplay.restype=C.c_void_p
X.XDefaultRootWindow.argtypes=[C.c_void_p];X.XDefaultRootWindow.restype=C.c_ulong
X.XFlush.argtypes=[C.c_void_p]
X.XSetInputFocus.argtypes=[C.c_void_p,C.c_ulong,C.c_int,C.c_ulong]
X.XRaiseWindow.argtypes=[C.c_void_p,C.c_ulong]
X.XMoveResizeWindow.argtypes=[C.c_void_p,C.c_ulong,C.c_int,C.c_int,C.c_uint,C.c_uint]
X.XStringToKeysym.argtypes=[C.c_char_p];X.XStringToKeysym.restype=C.c_ulong
X.XKeysymToKeycode.argtypes=[C.c_void_p,C.c_ulong];X.XKeysymToKeycode.restype=C.c_uint
X.XQueryTree.argtypes=[C.c_void_p,C.c_ulong,C.POINTER(C.c_ulong),C.POINTER(C.c_ulong),C.POINTER(C.POINTER(C.c_ulong)),C.POINTER(C.c_uint)]
X.XFetchName.argtypes=[C.c_void_p,C.c_ulong,C.POINTER(C.c_char_p)]
X.XFree.argtypes=[C.c_void_p]
X.XInternAtom.argtypes=[C.c_void_p,C.c_char_p,C.c_int];X.XInternAtom.restype=C.c_ulong
X.XGetWindowProperty.argtypes=[C.c_void_p,C.c_ulong,C.c_ulong,C.c_long,C.c_long,C.c_int,C.c_ulong,C.POINTER(C.c_ulong),C.POINTER(C.c_int),C.POINTER(C.c_ulong),C.POINTER(C.c_ulong),C.POINTER(C.c_void_p)]
X.XGetGeometry.argtypes=[C.c_void_p,C.c_ulong,C.POINTER(C.c_ulong),C.POINTER(C.c_int),C.POINTER(C.c_int),C.POINTER(C.c_uint),C.POINTER(C.c_uint),C.POINTER(C.c_uint),C.POINTER(C.c_uint)]
T.XTestFakeMotionEvent.argtypes=[C.c_void_p,C.c_int,C.c_int,C.c_int,C.c_ulong]
T.XTestFakeButtonEvent.argtypes=[C.c_void_p,C.c_uint,C.c_int,C.c_ulong]
T.XTestFakeKeyEvent.argtypes=[C.c_void_p,C.c_uint,C.c_int,C.c_ulong]
class XI(C.Structure):_fields_=[('width',C.c_int),('height',C.c_int),('xoffset',C.c_int),('format',C.c_int),('data',C.c_void_p),('byte_order',C.c_int),('bitmap_unit',C.c_int),('bitmap_bit_order',C.c_int),('bitmap_pad',C.c_int),('depth',C.c_int),('bytes_per_line',C.c_int),('bits_per_pixel',C.c_int)]
class WA(C.Structure):_fields_=[('x',C.c_int),('y',C.c_int),('width',C.c_int),('height',C.c_int),('border',C.c_int),('depth',C.c_int),('visual',C.c_void_p),('root',C.c_ulong),('cls',C.c_int),('bitgravity',C.c_int),('wingravity',C.c_int),('backingstore',C.c_int),('backingplanes',C.c_ulong),('backingpixel',C.c_ulong),('saveunder',C.c_int),('colormap',C.c_ulong),('mapinstalled',C.c_int),('mapstate',C.c_int),('allmasks',C.c_long),('yourmask',C.c_long),('dontpropagate',C.c_long),('override',C.c_int),('screen',C.c_void_p)]
X.XGetWindowAttributes.argtypes=[C.c_void_p,C.c_ulong,C.POINTER(WA)]
X.XGetImage.argtypes=[C.c_void_p,C.c_ulong,C.c_int,C.c_int,C.c_uint,C.c_uint,C.c_ulong,C.c_int];X.XGetImage.restype=C.POINTER(XI)
X.XDestroyImage.argtypes=[C.POINTER(XI)]
class Desktop:
 def __init__(self,display=b':187'):
  if not display.startswith(b':18'):raise ValueError('Only private test display range is permitted')
  self.d=X.XOpenDisplay(display)
  if not self.d:raise RuntimeError('Private Xvfb unavailable')
  self.root=X.XDefaultRootWindow(self.d)
 def click(self,x,y,button=1):
  if button not in (1,2):raise ValueError('Only primary and middle test clicks are supported')
  T.XTestFakeMotionEvent(self.d,-1,x,y,0);T.XTestFakeButtonEvent(self.d,button,1,0);T.XTestFakeButtonEvent(self.d,button,0,0);X.XFlush(self.d);time.sleep(.15)
 def focus(self,window):
  X.XRaiseWindow(self.d,window);X.XSetInputFocus(self.d,window,2,0);X.XFlush(self.d);time.sleep(.15)
 def fit(self,window):
  X.XMoveResizeWindow(self.d,window,0,0,900,700);X.XFlush(self.d);time.sleep(.3)
 def key(self,s,ctrl=False):
  sym=X.XStringToKeysym(s.encode());code=X.XKeysymToKeycode(self.d,sym)
  control=X.XKeysymToKeycode(self.d,X.XStringToKeysym(b'Control_L'))
  if ctrl:T.XTestFakeKeyEvent(self.d,control,1,0)
  T.XTestFakeKeyEvent(self.d,code,1,0);T.XTestFakeKeyEvent(self.d,code,0,0)
  if ctrl:T.XTestFakeKeyEvent(self.d,control,0,0)
  X.XFlush(self.d);time.sleep(.03)
 def type(self,s):
  for c in s:
   shift=c.isupper() or c in '_:!'
   low=c.lower() if c.isupper() else {'_':'-','!':'1',':':';',' ':'space','/':'slash','.':'period','-':'minus'}.get(c,c)
   code=X.XKeysymToKeycode(self.d,X.XStringToKeysym(low.encode()));sh=X.XKeysymToKeycode(self.d,X.XStringToKeysym(b'Shift_L'))
   if not code:raise RuntimeError('Unsupported synthetic test key')
   if shift:T.XTestFakeKeyEvent(self.d,sh,1,0)
   T.XTestFakeKeyEvent(self.d,code,1,0);T.XTestFakeKeyEvent(self.d,code,0,0)
   if shift:T.XTestFakeKeyEvent(self.d,sh,0,0)
  X.XFlush(self.d);time.sleep(.1)
 def windows(self):
  root=C.c_ulong();parent=C.c_ulong();children=C.POINTER(C.c_ulong)();n=C.c_uint();X.XQueryTree(self.d,self.root,C.byref(root),C.byref(parent),C.byref(children),C.byref(n));rows=[]
  for i in range(n.value):
   attr=WA()
   if not X.XGetWindowAttributes(self.d,children[i],C.byref(attr)) or attr.mapstate!=2:continue
   name=C.c_char_p();X.XFetchName(self.d,children[i],C.byref(name))
   actual=C.c_ulong();fmt=C.c_int();count=C.c_ulong();left=C.c_ulong();value=C.c_void_p();atom=X.XInternAtom(self.d,b'_NET_WM_NAME',0);X.XGetWindowProperty(self.d,children[i],atom,0,1024,0,0,C.byref(actual),C.byref(fmt),C.byref(count),C.byref(left),C.byref(value));title=C.string_at(value,count.value).decode(errors='replace') if value.value else (name.value.decode(errors='replace') if name.value else '');
   if title:rows.append((children[i],title))
   if value.value:X.XFree(value)
   if name.value:X.XFree(name)
  X.XFree(children);return rows
 def geometry(self,window):
  root=C.c_ulong();x=C.c_int();y=C.c_int();w=C.c_uint();h=C.c_uint();border=C.c_uint();depth=C.c_uint();X.XGetGeometry(self.d,window,C.byref(root),C.byref(x),C.byref(y),C.byref(w),C.byref(h),C.byref(border),C.byref(depth));return x.value,y.value,w.value,h.value
 def accept(self,prefix):
  window,_=self.wait(prefix);x,y,w,h=self.geometry(window);self.click(x+w-52,y+h-22)
 def inputs(self,prefix,values):
  window,_=self.wait(prefix,25);self.focus(window);x,y,w,h=self.geometry(window)
  # Private fresh-HOME test displays use the default light GTK theme. Locate
  # empty input rectangles rather than assuming the host's font/DPI spacing.
  with tempfile.NamedTemporaryFile(suffix='.png') as f:
   self.screenshot(f.name);im=Image.open(f.name).convert('RGB')
   rows=[yy for yy in range(max(0,y),min(1024,y+h-45)) if all(min(im.getpixel((xx,yy)))>250 for xx in [x+50,x+w//2,x+w-50])]
  groups=[]
  for yy in rows:
   if not groups or yy!=groups[-1][-1]+1:groups.append([])
   groups[-1].append(yy)
  centers=[g[len(g)//2] for g in groups if 12<=len(g)<=65]
  if len(centers)!=len(values):raise RuntimeError('Native masked entry layout could not be identified safely: '+str((centers,len(values))))
  for yy,value in zip(centers,values):self.click(x+200,yy);self.type(value)
 def wait(self,prefix,seconds=10):
  until=time.monotonic()+seconds
  while time.monotonic()<until:
   found=[w for w in self.windows() if w[1].startswith(prefix)]
   if found:return found[0]
   time.sleep(.1)
  raise RuntimeError('Expected native/public window did not appear: '+prefix)
 def screenshot(self,path):
  p=X.XGetImage(self.d,self.root,0,0,1280,1024,0xffffffff,2)
  try:
   data=C.string_at(p.contents.data,p.contents.bytes_per_line*1024);Image.frombytes('RGB',(1280,1024),data,'raw','BGRX',p.contents.bytes_per_line,1).save(path)
  finally:X.XDestroyImage(p)
