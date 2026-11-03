package lint

import (
	"github.com/sezznaw/devkit/internal/registry"
)

// registrySignedRule: idl/devkit.yaml 和它指向的 Redis 键登记表是授权（谁拥有哪些键、表、事件），由持有私钥的人签名
// （devkit registry sign → idl/registry.sig），这里用编译进 devkit 的公钥验签。别人改了登记表、或改了没重新签名，
// 每个服务的 lint 都报这一条；它不能用 devkit.yaml 的 disable 关掉（那个文件本身就在签名范围里）。
const registrySignedRule = "registry-signed"

func checkRegistrySigned(idlDir string) []Finding {
	if err := registry.Verify(idlDir, registry.TeamPublicKey()); err != nil {
		return []Finding{{"idl/" + registry.SigFile, 0, registrySignedRule,
			err.Error() + "。登记表只有 turtle 能改：把要加的行发给 turtle，由他改好并 `devkit registry sign` 后一起提交；不要自己改 devkit.yaml / redis-keys.txt"}}
	}
	return nil
}
