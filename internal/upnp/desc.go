package upnp

import "fmt"

func rootDesc(name, uuid, base string) string {
	return fmt.Sprintf(`<?xml version="1.0"?>
<root xmlns="urn:schemas-upnp-org:device-1-0"><specVersion><major>1</major><minor>0</minor></specVersion><URLBase>%s</URLBase>
<device><deviceType>urn:schemas-upnp-org:device:InternetGatewayDevice:1</deviceType><friendlyName>%s</friendlyName><manufacturer>Gatehouse</manufacturer><modelName>Gatehouse</modelName><UDN>uuid:%s</UDN>
<deviceList><device><deviceType>urn:schemas-upnp-org:device:WANDevice:1</deviceType><friendlyName>WAN Device</friendlyName><manufacturer>Gatehouse</manufacturer><modelName>Gatehouse</modelName><UDN>uuid:%s-wan</UDN>
<deviceList><device><deviceType>urn:schemas-upnp-org:device:WANConnectionDevice:1</deviceType><friendlyName>WAN Connection Device</friendlyName><manufacturer>Gatehouse</manufacturer><modelName>Gatehouse</modelName><UDN>uuid:%s-conn</UDN>
<serviceList><service><serviceType>urn:schemas-upnp-org:service:WANIPConnection:1</serviceType><serviceId>urn:upnp-org:serviceId:WANIPConn1</serviceId><SCPDURL>/WANIPCn.xml</SCPDURL><controlURL>/ctl/IPConn</controlURL><eventSubURL>/evt/IPConn</eventSubURL></service></serviceList>
</device></deviceList></device></deviceList></device></root>`, base, xmlEscape(name), uuid, uuid, uuid)
}

const scpd = `<?xml version="1.0"?>
<scpd xmlns="urn:schemas-upnp-org:service-1-0"><specVersion><major>1</major><minor>0</minor></specVersion>
<actionList>
<action><name>GetExternalIPAddress</name><argumentList><argument><name>NewExternalIPAddress</name><direction>out</direction><relatedStateVariable>ExternalIPAddress</relatedStateVariable></argument></argumentList></action>
<action><name>GetStatusInfo</name><argumentList><argument><name>NewConnectionStatus</name><direction>out</direction><relatedStateVariable>ConnectionStatus</relatedStateVariable></argument></argumentList></action>
<action><name>AddPortMapping</name><argumentList>
<argument><name>NewRemoteHost</name><direction>in</direction><relatedStateVariable>RemoteHost</relatedStateVariable></argument>
<argument><name>NewExternalPort</name><direction>in</direction><relatedStateVariable>ExternalPort</relatedStateVariable></argument>
<argument><name>NewProtocol</name><direction>in</direction><relatedStateVariable>PortMappingProtocol</relatedStateVariable></argument>
<argument><name>NewInternalPort</name><direction>in</direction><relatedStateVariable>InternalPort</relatedStateVariable></argument>
<argument><name>NewInternalClient</name><direction>in</direction><relatedStateVariable>InternalClient</relatedStateVariable></argument>
<argument><name>NewEnabled</name><direction>in</direction><relatedStateVariable>PortMappingEnabled</relatedStateVariable></argument>
<argument><name>NewPortMappingDescription</name><direction>in</direction><relatedStateVariable>PortMappingDescription</relatedStateVariable></argument>
<argument><name>NewLeaseDuration</name><direction>in</direction><relatedStateVariable>PortMappingLeaseDuration</relatedStateVariable></argument>
</argumentList></action>
<action><name>DeletePortMapping</name><argumentList>
<argument><name>NewRemoteHost</name><direction>in</direction><relatedStateVariable>RemoteHost</relatedStateVariable></argument>
<argument><name>NewExternalPort</name><direction>in</direction><relatedStateVariable>ExternalPort</relatedStateVariable></argument>
<argument><name>NewProtocol</name><direction>in</direction><relatedStateVariable>PortMappingProtocol</relatedStateVariable></argument>
</argumentList></action>
<action><name>GetGenericPortMappingEntry</name><argumentList><argument><name>NewPortMappingIndex</name><direction>in</direction><relatedStateVariable>PortMappingNumberOfEntries</relatedStateVariable></argument></argumentList></action>
<action><name>GetSpecificPortMappingEntry</name><argumentList><argument><name>NewExternalPort</name><direction>in</direction><relatedStateVariable>ExternalPort</relatedStateVariable></argument></argumentList></action>
</actionList>
<serviceStateTable>
<stateVariable sendEvents="no"><name>ExternalIPAddress</name><dataType>string</dataType></stateVariable>
<stateVariable sendEvents="no"><name>ConnectionStatus</name><dataType>string</dataType></stateVariable>
<stateVariable sendEvents="no"><name>RemoteHost</name><dataType>string</dataType></stateVariable>
<stateVariable sendEvents="no"><name>ExternalPort</name><dataType>ui2</dataType></stateVariable>
<stateVariable sendEvents="no"><name>InternalPort</name><dataType>ui2</dataType></stateVariable>
<stateVariable sendEvents="no"><name>PortMappingProtocol</name><dataType>string</dataType></stateVariable>
<stateVariable sendEvents="no"><name>InternalClient</name><dataType>string</dataType></stateVariable>
<stateVariable sendEvents="no"><name>PortMappingEnabled</name><dataType>boolean</dataType></stateVariable>
<stateVariable sendEvents="no"><name>PortMappingDescription</name><dataType>string</dataType></stateVariable>
<stateVariable sendEvents="no"><name>PortMappingLeaseDuration</name><dataType>ui4</dataType></stateVariable>
<stateVariable sendEvents="no"><name>PortMappingNumberOfEntries</name><dataType>ui2</dataType></stateVariable>
</serviceStateTable></scpd>`
