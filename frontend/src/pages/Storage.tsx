import { PageTitle, StatusLabel } from '../ui';

export function Storage() {
  return <div className="space-y-4"><PageTitle>存储</PageTitle><section className="panel"><StatusLabel state="disabled">未接入</StatusLabel></section></div>;
}
