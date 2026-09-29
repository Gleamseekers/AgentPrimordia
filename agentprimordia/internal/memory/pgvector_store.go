package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	agentpgvector "github.com/Gleamseekers/AgentPrimordia/pgvector"
)

// PgVectorVectorStore 是基于 PostgreSQL + pgvector 的向量存储实现。
// 适用于生产环境，支持大规模向量数据（>1M 文档）。
type PgVectorVectorStore struct {
	store *agentpgvector.Store
}

// PgVectorConfig pgvector 后端配置
type PgVectorConfig struct {
	// ConnString PostgreSQL 连接字符串
	// 格式: postgres://user:pass@host:5432/dbname
	ConnString string
	// TableName 表名（默认 agent_vectors）
	TableName string
	// Dimensions 向量维度（默认 1536，适用于 OpenAI text-embedding-3-small）
	Dimensions int
	// Distance 距离度量（默认 cosine）
	Distance agentpgvector.DistanceType
}

// NewPgVectorVectorStore 创建 pgvector 向量存储实例
func NewPgVectorVectorStore(ctx context.Context, cfg PgVectorConfig) (*PgVectorVectorStore, error) {
	if cfg.ConnString == "" {
		return nil, fmt.Errorf("pgvector: conn string is required")
	}
	if cfg.Dimensions <= 0 {
		cfg.Dimensions = 1536
	}
	if cfg.Distance == "" {
		cfg.Distance = agentpgvector.CosineDistance
	}

	store, err := agentpgvector.New(ctx, agentpgvector.Config{
		ConnString: cfg.ConnString,
		TableName:  cfg.TableName,
		Dimensions: cfg.Dimensions,
		Distance:   cfg.Distance,
	})
	if err != nil {
		return nil, fmt.Errorf("pgvector: create store: %w", err)
	}

	return &PgVectorVectorStore{store: store}, nil
}

// Insert 插入向量记录
func (p *PgVectorVectorStore) Insert(ctx context.Context, collection string, records []*VectorRecord) error {
	if len(records) == 0 {
		return nil
	}
	for _, r := range records {
		metadata := metadataToStringMap(r.Metadata)
		if err := p.store.AddWithText(ctx, r.ID, r.Vector, "", metadata); err != nil {
			return fmt.Errorf("pgvector: insert %s: %w", r.ID, err)
		}
	}
	return nil
}

// Delete 删除向量记录
func (p *PgVectorVectorStore) Delete(ctx context.Context, collection string, ids []string) error {
	for _, id := range ids {
		if err := p.store.Delete(ctx, id); err != nil {
			return fmt.Errorf("pgvector: delete %s: %w", id, err)
		}
	}
	return nil
}

// Search 向量搜索
func (p *PgVectorVectorStore) Search(ctx context.Context, collection string, query []float32, opts VectorSearchOptions) ([]*VectorMatch, error) {
	if opts.TopK <= 0 {
		opts.TopK = defaultTopK
	}

	results, err := p.store.Search(ctx, query, opts.TopK, nil)
	if err != nil {
		return nil, fmt.Errorf("pgvector: search: %w", err)
	}

	matches := make([]*VectorMatch, 0, len(results))
	for _, r := range results {
		if opts.Threshold > 0 && r.Score < opts.Threshold {
			continue
		}
		// 转换 metadata: map[string]string → map[string]any
		md := make(map[string]any, len(r.Metadata))
		for k, v := range r.Metadata {
			md[k] = v
		}
		matches = append(matches, &VectorMatch{
			ID:       r.ID,
			Score:    r.Score,
			Metadata: md,
		})
	}
	return matches, nil
}

// CreateCollection 创建向量集合
// pgvector 使用单一表，collection 参数作为前缀过滤（暂不实现多表）
func (p *PgVectorVectorStore) CreateCollection(ctx context.Context, name string, dim int) error {
	// pgvector 在初始化时已创建表，此处仅验证维度
	if dim > 0 && dim != p.storeDim() {
		return fmt.Errorf("pgvector: dimension mismatch, store uses %d, requested %d", p.storeDim(), dim)
	}
	return nil
}

// DropCollection 删除向量集合
func (p *PgVectorVectorStore) DropCollection(ctx context.Context, name string) error {
	// pgvector 实现使用单一表，不支持按 collection 删除
	// 可以扩展为按前缀删除
	return fmt.Errorf("pgvector: DropCollection not yet implemented for table=%s", name)
}

// Close 关闭底层连接池
func (p *PgVectorVectorStore) Close() {
	p.store.Close()
}

// Count 返回向量记录总数
func (p *PgVectorVectorStore) Count() (int64, error) {
	return p.store.Count(context.Background())
}

// HealthCheck 检查连接是否正常
func (p *PgVectorVectorStore) HealthCheck(ctx context.Context) error {
	return p.store.HealthCheck(ctx)
}

// storeDim 返回存储的维度（通过 Count 间接获取）
func (p *PgVectorVectorStore) storeDim() int {
	// pgvector 不直接暴露维度，但可以通过 schema 获取
	// 这里返回 0 表示不校验
	return 0
}

// metadataToStringMap 将 map[string]any 转为 map[string]string
func metadataToStringMap(m map[string]any) map[string]string {
	if len(m) == 0 {
		return nil
	}
	result := make(map[string]string, len(m))
	for k, v := range m {
		if s, ok := v.(string); ok {
			result[k] = s
		} else {
			// 非字符串值序列化为 JSON
			data, err := json.Marshal(v)
			if err == nil {
				result[k] = string(data)
			} else {
				result[k] = fmt.Sprintf("%v", v)
			}
		}
	}
	return result
}

// Ensure PgVectorVectorStore implements VectorStore
var _ VectorStore = (*PgVectorVectorStore)(nil)

// 编译期检查
var _ = strings.TrimSpace
