-- 文件作用：区分删后新附件证明与在线资料证明，旧客户端不能借在线建档收据回写被删除的旧资料。
ALTER TABLE candidate_reopen_receipts ADD COLUMN proof_type text NOT NULL DEFAULT 'attachment'
 CHECK(proof_type IN ('attachment','online_profile'));
COMMENT ON COLUMN candidate_reopen_receipts.proof_type IS '本代重新建档依据：新附件或已核实的新在线资料；旧记录保持附件规则';
ALTER TABLE candidate_reopen_receipts ADD COLUMN profile_key text NOT NULL DEFAULT '';
COMMENT ON COLUMN candidate_reopen_receipts.profile_key IS '在线重新建档的服务器资料指纹；附件收据为空';
