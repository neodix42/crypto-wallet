package ton

import (
	"bytes"
	"errors"

	"localwallet/internal/model"
)

// ProfileSubwallet42 is a separate, explicit recovery identity. Changing a
// wallet's subwallet number changes its StateInit and receiving address; it can
// never be applied to an existing or already funded profile in place.
const ProfileSubwallet42 = "ton-bip39-slip10-v5r1-subwallet42-v2"

// New accounts use the owner's requested subwallet 42 through the standard
// network-specific V5 client context. Recorded V1 accounts remain unchanged.
const DefaultProfile = ProfileSubwallet42

func profileWalletID(profile string, globalID int32) (uint16, uint32, error) {
	var subwallet uint16
	switch profile {
	case Profile:
	case ProfileSubwallet42:
		subwallet = 42
	default:
		return 0, 0, errors.New("unsupported immutable TON recovery profile")
	}
	// Official Wallet V5R1 client context: high client bit, workchain 0,
	// version discriminator 0, 15-bit subwallet number. Preserve the selected
	// network's replay domain instead of using raw wallet_id 42 on both chains.
	return subwallet, uint32(globalID) ^ (uint32(1)<<31 | uint32(subwallet)), nil
}

// ValidateRecovery preserves the exact registered contract identity when a
// profile default evolves. It never rewrites account parameters or addresses.
func ValidateRecovery(seed []byte, account model.Account) error {
	if _, _, err := descriptor(account); err != nil {
		return err
	}
	expected, err := New().DeriveProfile(seed, account.Index, account.Network, account.Profile)
	if err != nil || expected.ID != account.ID || expected.PublicKey != account.PublicKey || expected.Address != account.Address || !bytes.Equal(expected.Parameters, account.Parameters) {
		return errors.New("TON recorded contract does not belong to this recovery root/profile")
	}
	return nil
}
