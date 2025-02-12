package wapsnmp

/* This file implements BER ASN1 encoding and decoding.

References : http://rane.com/note161.html

This package was made due to the inability of the encoding/asn1 library to
parse SNMP packets received from actual network devices. In order to fix
encoding/asn1 I would need to make deep changes in that core library file.

First difference is that this file works differently from the standard
libary one : this will convert between []interface{} and ASN1, whereas
encoding/asn1 converts between structs and ASN1.

Furthermore encoding/asn1 is an implementation of DER, whereas this does BER
(DER is a subset of BER). They're different like xml and html are different.
In theory html should be valid xml, in practice it's not. This means you can't
use an existing xml parser to parse html if you communicate with external
devices, because it wouldn't parse. Likewise you can't use a DER parser to
parse BER.
*/

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// Constants for the Type of the TLV field.
type BERType uint8

// Constants for the different types of the TLV fields.
const (
	AsnBoolean     BERType = 0x01
	AsnInteger     BERType = 0x02
	AsnBitStr      BERType = 0x03
	AsnOctetStr    BERType = 0x04
	AsnNull        BERType = 0x05
	AsnObjectID    BERType = 0x06
	AsnSequence    BERType = 0x10
	AsnSet         BERType = 0x11
	AsnUniversal   BERType = 0x00
	AsnApplication BERType = 0x40
	AsnContext     BERType = 0x80
	AsnPrivate     BERType = 0xC0
	AsnPrimitive   BERType = 0x00
	AsnConstructor BERType = 0x20

	AsnLongLen     BERType = 0x80
	AsnExtensionID BERType = 0x1F
	AsnBit8        BERType = 0x80

	Integer     BERType = AsnUniversal | 0x02
	Integer32   BERType = AsnUniversal | 0x02
	Bitstring   BERType = AsnUniversal | 0x03
	Octetstring BERType = AsnUniversal | 0x04
	Null        BERType = AsnUniversal | 0x05
	UOid        BERType = AsnUniversal | 0x06
	Sequence    BERType = AsnConstructor | 0x10

	Ipaddress  BERType = AsnApplication | 0x00
	Counter    BERType = AsnApplication | 0x01
	Counter32  BERType = AsnApplication | 0x01
	Gauge      BERType = AsnApplication | 0x02
	Gauge32    BERType = AsnApplication | 0x02
	Timeticks  BERType = AsnApplication | 0x03
	Opaque     BERType = AsnApplication | 0x04
	Counter64  BERType = AsnApplication | 0x06
	Uinteger32 BERType = AsnApplication | 0x07

	AsnGetRequest     BERType = 0xa0
	AsnGetNextRequest BERType = 0xa1
	AsnGetResponse    BERType = 0xa2
	AsnSetRequest     BERType = 0xa3
	AsnTrap           BERType = 0xa4
	AsnGetBulkRequest BERType = 0xa5
	AsnInform         BERType = 0xa6
	AsnTrap2          BERType = 0xa7
	AsnReport         BERType = 0xa8

	NoSuchObject   BERType = 0x80
	NoSuchInstance BERType = 0x81
	EndOfMibView   BERType = 0x82
)

// Type to indicate which SNMP version is in use.
type SNMPVersion uint8

// List the supported snmp versions.
const (
	SNMPv1  SNMPVersion = 0
	SNMPv2c SNMPVersion = 1
	SNMPv3  SNMPVersion = 3
)

// EncodeLength encodes an integer value as a BER compliant length value.
func EncodeLength(length int) []byte {
	// The first bit is used to indicate whether this is the final byte
	// encoding the length. So, if the first bit is 0, just return a one
	// byte response containing the byte-encoded length.
	if length <= 0x7f {
		return []byte{byte(length)}
	}

	// If the length is bigger the format is, first bit 1 + the rest of the
	// bits in the first byte encode the length of the length, then follows
	// the actual length.

	// Technically the SNMP spec allows for packet lengths longer than can be
	// specified in a 127-byte encoded integer, however, going out on a limb
	// here, I don't think I'm going to support a use case that insane.

	r := EncodeInteger(length)
	numOctets := len(r)
	result := make([]byte, 1+numOctets)
	result[0] = 0x80 | byte(numOctets)
	for i, b := range r {
		result[1+i] = b
	}
	return result
}

/* DecodeLength returns the length and the length of the length or an error.

   Caveats: Does not support indefinite length. Couldn't find any
   SNMP packet dump actually using that.
*/
func DecodeLength(toparse []byte) (int, int, error) {
	// If the first bit is zero, the rest of the first byte indicates the length. Values up to 127 are encoded this way (unless you're using indefinite length, but we don't support that)

	if toparse[0] == 0x80 {
		return 0, 0, fmt.Errorf("we don't support indefinite length encoding")
	}
	if toparse[0]&0x80 == 0 {
		return int(toparse[0]), 1, nil
	}

	// If the first bit is one, the rest of the first byte encodes the length of then encoded length. So read how many bytes are part of the length.
	numOctets := int(toparse[0] & 0x7f)
	if len(toparse) < 1+numOctets {
		return 0, 0, fmt.Errorf("invalid length")
	}

	// Decode the specified number of bytes as a BER Integer encoded
	// value.
	val, err := DecodeInteger(toparse[1 : numOctets+1])
	if err != nil {
		return 0, 0, err
	}

	return val, 1 + numOctets, nil
}

func DecodeCounter64(toparse []byte) (uint64, error) {
	if len(toparse) > 8 {
		return 0, fmt.Errorf("Does not support more than 64 bits")
	}
	var val uint64
	val = 0
	for _, b := range toparse {
		val = val*256 + uint64(b)
	}
	return val, nil
}

// DecodeInt64 treats the given bytes as a big-endian, signed integer and
// returns the result.
func DecodeInt64(bytes []byte) (ret int64, err error) {
	if len(bytes) > 8 {
		// We'll overflow an int64 in this case.
		err = errors.New("Does not support more than 64 bits")
		return
	}
	for bytesRead := 0; bytesRead < len(bytes); bytesRead++ {
		ret <<= 8
		ret |= int64(bytes[bytesRead])
	}

	// Shift up and down in order to sign extend the result.
	ret <<= 64 - uint8(len(bytes))*8
	ret >>= 64 - uint8(len(bytes))*8
	return
}

/* DecodeInteger decodes an integer. This does not handle signed value.

   Will error out if it's longer than 64 bits. */
func DecodeInteger(toparse []byte) (int, error) {
	if len(toparse) > 8 {
		return 0, fmt.Errorf("Does not support more than 64 bits")
	}
	val := 0
	for _, b := range toparse {
		val = val*256 + int(b)
	}
	return val, nil
}

func DecodeIPAddress(toparse []byte) (string, error) {
	if len(toparse) != 4 {
		return "", fmt.Errorf("need 4 bytes for IP address")
	}
	return fmt.Sprintf("%d.%d.%d.%d", toparse[0], toparse[1], toparse[2], toparse[3]), nil
}

func DecodeTimeticks(toparse []byte) (string, error) {
	val, err := DecodeInteger(toparse)
	if err != nil {
		return "", err
	}
	//ticks := val

	days := val / 8640000
	val %= 8640000
	hours := val / 360000
	val %= 360000
	minutes := val / 6000
	val %= 6000
	seconds := val / 100
	val %= 100

	if days > 0 {
		return fmt.Sprintf("%02d days %02d:%02d:%02d.%02d", days, hours, minutes, seconds, val), nil
	} else {
		return fmt.Sprintf("%02d:%02d:%02d.%02d", hours, minutes, seconds, val), nil
	}
}

// EncodeInteger encodes an integer to BER format.
func EncodeInteger(toEncode int) []byte {
	if toEncode == 0 {
		return []byte{0}
	}

	// For negative numbers
	negativeInteger := make([]byte, 4)
	if -2147483648 <= toEncode && toEncode < 0 {
		toEncode = ^toEncode
		binary.BigEndian.PutUint32(negativeInteger, uint32(toEncode))
		for k, v := range negativeInteger {
			negativeInteger[k] = ^v
		}
		return negativeInteger
	}

	result := make([]byte, 8)
	pos := 7
	i := toEncode
	for i > 0 {
		result[pos] = byte(i % 256)
		i = i >> 8
		pos--
	}
	if result[pos+1] >= 0x80 {
		result[pos] = 0x00
		pos--
	}
	return result[pos+1 : 8]
}

// EncodeUInteger32 encodes an Unsigned32 integer to BER format.
func EncodeUInteger32(toEncode uint32) []byte {
	if toEncode == 0 {
		return []byte{0}
	}
	result := make([]byte, 4)
	pos := 3
	i := toEncode
	for i > 0 {
		result[pos] = byte(i % 256)
		i = i >> 8
		pos--
	}

	return result[pos+1 : 4]
}

// DecodeSequence decodes BER binary data into *[]interface{}.
func DecodeSequence(toparse []byte) ([]interface{}, error) {
	var result []interface{}

	if len(toparse) < 2 {
		return nil, fmt.Errorf("sequence cannot be shorter than 2 bytes")
	}
	sqType := BERType(toparse[0])
	result = append(result, sqType)
	// Bit 6 is the P/C primitive/constructed bit. Which means it's a set, essentially.
	if sqType != Sequence && (toparse[0]&0x20 == 0) {
		return nil, fmt.Errorf("byte array parsed in is not a sequence")
	}
	seqLength, seqLenLen, err := DecodeLength(toparse[1:])
	if err != nil {
		return nil, fmt.Errorf("failed to parse sequence length %v", seqLenLen)
	}

	if seqLength == 0 {
		return result, nil
	}

	lidx := 0
	idx := 1 + seqLenLen
	toparse = toparse[:(1 + seqLenLen + seqLength)]
	// Let's guarantee progress.
	for idx < len(toparse) && idx > lidx {
		berType := toparse[idx]
		berLength, berLenLen, err := DecodeLength(toparse[idx+1:])
		if err != nil {
			return nil, fmt.Errorf("length parse error @ idx %v", idx)
		}
		berValue := toparse[idx+1+berLenLen : idx+1+berLenLen+berLength]
		berAll := toparse[idx : idx+1+berLenLen+berLength]

		switch BERType(berType) {
		case AsnBoolean:
			if berLength != 1 {
				return nil, fmt.Errorf("boolean length != 1 @ idx %v", idx)
			}
			result = append(result, berValue[0] == 0)
		case AsnInteger:
			ret64, err := DecodeInt64(berValue)
			if err != nil {
				return nil, fmt.Errorf("error in DecodeInt64:%v", err.Error())
			}
			result = append(result, int(ret64))
		case AsnOctetStr:
			result = append(result, string(berValue))
		case AsnNull:
			result = append(result, nil)
		case AsnObjectID:
			oid, err := DecodeOid(berValue)
			if err != nil {
				return nil, err
			}
			result = append(result, oid.String())
		case Gauge32, Counter32:
			val, err := DecodeInteger(berValue)
			if err != nil {
				return nil, err
			}
			result = append(result, val)
		case Counter64:
			val, err := DecodeCounter64(berValue)
			if err != nil {
				return nil, err
			}
			result = append(result, val)

		case Timeticks:
			val, err := DecodeTimeticks(berValue)
			if err != nil {
				return nil, err
			}
			result = append(result, val)
		case Ipaddress:
			val, err := DecodeIPAddress(berValue)
			if err != nil {
				return nil, err
			}
			result = append(result, val)
		case Sequence:
			pdu, err := DecodeSequence(berAll)
			if err != nil {
				return nil, err
			}
			result = append(result, pdu)
		case AsnGetNextRequest, AsnGetRequest, AsnGetResponse, AsnReport, AsnTrap2, AsnTrap:
			pdu, err := DecodeSequence(berAll)
			if err != nil {
				return nil, err
			}
			result = append(result, pdu)
		case NoSuchInstance:
			result = append(result, nil)
			result = append(result, "NoSuchInstance")
		case NoSuchObject:
			result = append(result, nil)
			result = append(result, "NoSuchObject")
		case EndOfMibView:
			result = append(result, nil)
			result = append(result, "EndOfMibView")
		default:
			return nil, fmt.Errorf("did not understand type %v", berType)
		}

		lidx = idx
		idx = idx + 1 + berLenLen + berLength
	}

	return result, nil
}

// EncodeSequence will encode an []interface{} into an SNMP bytestream.
func EncodeSequence(toEncode []interface{}) ([]byte, error) {
	switch toEncode[0].(type) {
	default:
		return nil, fmt.Errorf("first element of sequence to encode should be sequence type")
	case BERType:
		// OK
	}

	seqType := toEncode[0].(BERType)
	var toEncap []byte
	for _, val := range toEncode[1:] {
		switch val := val.(type) {
		default:
			return nil, fmt.Errorf("couldn't handle type %T", val)
		case nil:
			toEncap = append(toEncap, byte(AsnNull))
			toEncap = append(toEncap, 0)
		case int:
			enc := EncodeInteger(val)
			// TODO encode length ?
			toEncap = append(toEncap, byte(AsnInteger))
			toEncap = append(toEncap, byte(len(enc)))
			for _, b := range enc {
				toEncap = append(toEncap, b)
			}
		case uint32:
			enc := EncodeUInteger32(val)
			// TODO encode length ?
			toEncap = append(toEncap, byte(Uinteger32))
			toEncap = append(toEncap, byte(len(enc)))
			for _, b := range enc {
				toEncap = append(toEncap, b)
			}
		case string:
			enc := []byte(val)
			toEncap = append(toEncap, byte(AsnOctetStr))
			for _, b := range EncodeLength(len(enc)) {
				toEncap = append(toEncap, b)
			}
			for _, b := range enc {
				toEncap = append(toEncap, b)
			}
		case Oid:
			enc, err := val.Encode()
			if err != nil {
				return nil, err
			}
			toEncap = append(toEncap, byte(AsnObjectID))
			encLen := EncodeLength(len(enc))
			for _, b := range encLen {
				toEncap = append(toEncap, b)
			}
			for _, b := range enc {
				toEncap = append(toEncap, b)
			}
		case IPAddress:
			toEncap = append(toEncap, byte(Ipaddress))
			for _, b := range EncodeLength(len(val)) {
				toEncap = append(toEncap, b)
			}
			for _, b := range val {
				toEncap = append(toEncap, b)
			}
		case []interface{}:
			enc, err := EncodeSequence(val)
			if err != nil {
				return nil, err
			}
			for _, b := range enc {
				toEncap = append(toEncap, b)
			}
		}
	}

	l := EncodeLength(len(toEncap))
	// Encode length ...
	result := []byte{byte(seqType)}
	for _, b := range l {
		result = append(result, b)
	}
	for _, b := range toEncap {
		result = append(result, b)
	}
	return result, nil
}
