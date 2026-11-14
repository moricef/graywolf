"""Explicit LoRa settings for the controller and the authenticated RF request."""
from dataclasses import dataclass,asdict
import json
import math
import struct

@dataclass(frozen=True)
class RadioProfile:
    frequency_hz:int
    bandwidth_hz:int
    spreading_factor:int
    coding_rate:int
    power_dbm:int

    @classmethod
    def from_mapping(cls,data):
        fields=cls.__dataclass_fields__
        if not isinstance(data,dict) or set(data)!=set(fields):raise ValueError('profile fields must be '+', '.join(fields))
        if any(type(v) is not int for v in data.values()):raise ValueError('radio profile values must be integers')
        p=cls(**data)
        if not (137000000<=p.frequency_hz<=1020000000 and
                p.bandwidth_hz in (7800,10400,15600,20800,31250,41700,62500,125000,250000,500000) and
                5<=p.spreading_factor<=12 and 5<=p.coding_rate<=8 and -9<=p.power_dbm<=22):
            raise ValueError('radio profile outside supported bounds')
        return p

    def encode(self):
        return struct.pack('>IIBBb',self.frequency_hz,self.bandwidth_hz,self.spreading_factor,self.coding_rate,self.power_dbm)

    def bridge_command(self,index):
        return ('PROFILEDEF %d %d %d %d %d %d\n'%(index,self.frequency_hz,self.bandwidth_hz,
                self.spreading_factor,self.coding_rate,self.power_dbm)).encode()

    def airtime(self,length=255):
        # Explicit header, CRC, 8-symbol preamble; conservative margin is added by callers.
        sf=self.spreading_factor;symbol=(1<<sf)/self.bandwidth_hz
        de=int(symbol>=.016)
        payload=8+max(math.ceil((8*length-4*sf+28+16)/(4*(sf-2*de)))*self.coding_rate,0)
        return (8+4.25+payload)*symbol

    def mapping(self):return asdict(self)

def load_profiles(path):
    data=json.loads(path.read_text())
    if not isinstance(data,dict) or set(data)!={'normal','transfer'}:
        raise ValueError('radio profile file requires normal and transfer')
    return {name:RadioProfile.from_mapping(p) for name,p in data.items()}
