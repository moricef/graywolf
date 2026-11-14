"""Authenticated two-hop envelope. Firmware MAC remains end-to-end."""
import hmac
PREFIX=b'\x3c\xff\x7d\x01'
DOMAIN=b'LORA-APRS-OTA-HOP1'
CHUNK=175
class RouteTransport:
    def __init__(self,key,source=b'F4MLV-2',target=b'F4MLV-15',relay=b'F4MLV-10'):
        if len(key)!=32:raise ValueError('route key must be 32 bytes')
        self.key=key;self.addresses=[source,relay,target,relay]
        if any(not 1<=len(x)<=8 for x in self.addresses):raise ValueError('route identity size')
    def encode(self,inner,phase=0):
        if not 51<=len(inner)<=226 or not 0<=phase<=3:raise ValueError('route frame size/phase')
        raw=PREFIX+bytes([phase])+self.addresses[phase].ljust(8,b'\0')+inner
        return raw+hmac.digest(self.key,DOMAIN+raw,'sha256')[:16]
    def decode(self,wire,phase=3):
        if not 80<=len(wire)<=255 or wire[:4]!=PREFIX or wire[4]!=phase:return None
        if not hmac.compare_digest(wire[-16:],hmac.digest(self.key,DOMAIN+wire[:-16],'sha256')[:16]):return None
        if wire[5:13]!=self.addresses[phase].ljust(8,b'\0'):return None
        inner=wire[13:-16]
        if inner[:4]!=b'\x3c\xff\x7e\x02' or len(inner)!=51+int.from_bytes(inner[17:19],'big'):return None
        return inner
