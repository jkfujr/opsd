import { PageHead, Sheet, Empty } from '../components/ui';

export function Storage() {
  return (
    <div className="page">
      <PageHead title="存储" meta={['Silo 未接入', '本批保留方向']} />
      <Sheet>
        <Empty>存储功能尚未建设</Empty>
      </Sheet>
    </div>
  );
}
